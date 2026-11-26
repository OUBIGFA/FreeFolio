package localresource

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxUpscaleOutputPixels = 64 << 20
	maxUpscaleOutputSide   = 16384
	maxArchiveMemberBytes  = 128 << 20
)

var upscaleInference = make(chan struct{}, 1)

// Upscale enlarges an image locally with Real-ESRGAN (ncnn-vulkan) by 2x or 4x.
// Model inference runs at native 4x; 2x supersamples the 4x result cleanly.
func Upscale(ctx context.Context, root, modelID string, scale int, input []byte) ([]byte, error) {
	if _, ok := UpscaleModel(modelID); !ok {
		return nil, errors.New("unknown upscale model")
	}
	if scale == 0 {
		scale = 4
	}
	if scale != 2 && scale != 4 {
		return nil, errors.New("upscale factor must be 2 or 4")
	}
	if len(input) > maxInput {
		return nil, errors.New("input image exceeds 64 MiB")
	}
	pkg, ok := UpscaleRuntime()
	if !ok {
		return nil, errors.New("no local upscale engine for this platform")
	}
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	origW, origH := img.Bounds().Dx(), img.Bounds().Dy()
	if origW <= 0 || origH <= 0 {
		return nil, errors.New("empty input image")
	}
	outW, outH := origW*scale, origH*scale
	if int64(outW)*int64(outH) > maxUpscaleOutputPixels || outW > maxUpscaleOutputSide || outH > maxUpscaleOutputSide {
		return nil, errors.New("upscaled image exceeds size limit")
	}
	select {
	case upscaleInference <- struct{}{}:
		defer func() { <-upscaleInference }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	archive, err := verified(root, pkg)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	zr, err := zip.NewReader(archive, pkg.Size)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "freefolio-upscale-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	modelsDir := filepath.Join(scratch, "models")
	if err := os.MkdirAll(modelsDir, 0700); err != nil {
		return nil, err
	}
	wanted := map[string]struct {
		dst  string
		mode os.FileMode
	}{
		pkg.member:                     {filepath.Join(scratch, filepath.Base(pkg.member)), 0700},
		"vcomp140.dll":                 {filepath.Join(scratch, "vcomp140.dll"), 0600},
		"vcomp140d.dll":                {filepath.Join(scratch, "vcomp140d.dll"), 0600},
		"models/" + modelID + ".bin":   {filepath.Join(modelsDir, modelID+".bin"), 0600},
		"models/" + modelID + ".param": {filepath.Join(modelsDir, modelID+".param"), 0600},
	}
	for _, f := range zr.File {
		spec, match := wanted[f.Name]
		if !match {
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("unexpected archive entry: %s", f.Name)
		}
		if err := extractZipEntry(f, spec.dst, spec.mode); err != nil {
			return nil, err
		}
	}
	exePath := filepath.Join(scratch, filepath.Base(pkg.member))
	for _, req := range []string{exePath, filepath.Join(modelsDir, modelID+".bin"), filepath.Join(modelsDir, modelID+".param")} {
		if _, err := os.Stat(req); err != nil {
			return nil, fmt.Errorf("missing upscale resource %s: %w", filepath.Base(req), err)
		}
	}

	inPath := filepath.Join(scratch, "input.png")
	outPath := filepath.Join(scratch, "output.png")
	if err := writeNormalizedPNG(inPath, img); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, exePath, "-i", inPath, "-o", outPath, "-m", modelsDir, "-n", modelID, "-s", "4", "-f", "png")
	cmd.Dir = scratch
	hideConsole(cmd)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(outBuf.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	outBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if scale == 2 {
		return resizePNG(outBytes, outW, outH)
	}
	return outBytes, nil
}

func extractZipEntry(f *zip.File, dst string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(rc, maxArchiveMemberBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxArchiveMemberBytes {
		return fmt.Errorf("archive entry too large: %s", f.Name)
	}
	return nil
}

func writeNormalizedPNG(path string, img image.Image) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	encErr := png.Encode(out, img)
	closeErr := out.Close()
	if encErr != nil {
		return encErr
	}
	return closeErr
}

func resizePNG(data []byte, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, errors.New("invalid target dimensions")
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
