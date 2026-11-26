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
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	ort "github.com/getcharzp/onnxruntime_purego"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const defaultSide = 1024
const maxInput = 64 << 20

var inference = make(chan struct{}, 1)

func Cutout(ctx context.Context, root, id string, input []byte) ([]byte, error) {
	if len(input) > maxInput {
		return nil, errors.New("input image exceeds 64 MiB")
	}
	select {
	case inference <- struct{}{}:
		defer func() { <-inference }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "freefolio-cutout-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	cmd := exec.CommandContext(ctx, executable, "cutout-worker", root, id, scratch)
	cmd.Stdin = bytes.NewReader(input)
	data, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(failure.Stderr)))
		}
		return nil, err
	}
	return data, nil
}

func Worker(root, id, scratch string, stdin io.Reader, stdout io.Writer) error {
	model, ok := Model(id)
	if !ok {
		return errors.New("unknown cutout model")
	}
	runtimeFile, ok := Runtime()
	if !ok {
		return errors.New("no local runtime for this platform")
	}
	encoded, err := io.ReadAll(io.LimitReader(stdin, maxInput+1))
	if err != nil {
		return err
	}
	if len(encoded) > maxInput {
		return errors.New("input image exceeds 64 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32<<20 {
		return errors.New("input image exceeds 32 megapixels")
	}
	original, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	archive, err := verified(root, runtimeFile)
	if err != nil {
		return err
	}
	defer archive.Close()
	reader, err := zip.NewReader(archive, runtimeFile.Size)
	if err != nil {
		return err
	}
	member, err := reader.Open(runtimeFile.member)
	if err != nil {
		return err
	}
	defer member.Close()
	library, err := os.CreateTemp(scratch, "ort-*"+filepath.Ext(runtimeFile.member))
	if err != nil {
		return err
	}
	defer os.Remove(library.Name())
	size, err := io.Copy(library, io.LimitReader(member, (64<<20)+1))
	closeErr := library.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if size > 64<<20 {
		return errors.New("runtime library is too large")
	}
	file, err := verified(root, model)
	if err != nil {
		return err
	}
	defer file.Close()
	weights, err := io.ReadAll(io.LimitReader(file, model.Size+1))
	if err != nil {
		return err
	}
	engine, err := ort.NewEngine(library.Name())
	if err != nil {
		return err
	}
	defer engine.Close()
	options, err := engine.NewSessionOptions()
	if err != nil {
		return err
	}
	defer options.Close()
	if err = options.SetIntraOpNumThreads(min(4, runtime.GOMAXPROCS(0))); err != nil {
		return err
	}
	session, err := engine.NewSessionFromBytes(weights, options)
	if err != nil {
		return err
	}
	defer session.Close()
	if len(session.Inputs) != 1 || len(session.Outputs) != 1 {
		return errors.New("unexpected cutout model signature")
	}
	modelSide := model.Side
	if modelSide <= 0 {
		modelSide = defaultSide
	}
	input, err := ort.NewTensor([]int64{1, 3, int64(modelSide), int64(modelSide)}, pixels(original, modelSide))
	if err != nil {
		return err
	}
	defer input.Close()
	outputs, err := session.Run(map[string]*ort.Value{session.Inputs[0].Name: input})
	if err != nil {
		return err
	}
	for _, value := range outputs {
		defer value.Close()
	}
	mask, err := ort.GetTensorData[float32](outputs[session.Outputs[0].Name])
	if err != nil {
		return err
	}
	result, err := applyMask(original, mask, modelSide, modelSide, model.ProbOutput)
	if err != nil {
		return err
	}
	return png.Encode(stdout, result)
}

func pixels(src image.Image, modelSide int) []float32 {
	resized := image.NewRGBA(image.Rect(0, 0, modelSide, modelSide))
	draw.Draw(resized, resized.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(resized, resized.Bounds(), src, src.Bounds(), draw.Over, nil)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	plane := modelSide * modelSide
	out := make([]float32, 3*plane)
	for index := range plane {
		for channel := range 3 {
			out[channel*plane+index] = (float32(resized.Pix[index*4+channel])/255 - mean[channel]) / std[channel]
		}
	}
	return out
}

func applyMask(src image.Image, values []float32, width, height int, probOutput bool) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 || len(values) != width*height {
		return nil, errors.New("unexpected cutout mask size")
	}
	isLogits := false
	if !probOutput {
		minVal, maxVal := values[0], values[0]
		for _, value := range values {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, errors.New("invalid cutout mask")
			}
			if value < minVal {
				minVal = value
			}
			if value > maxVal {
				maxVal = value
			}
		}
		isLogits = minVal < -0.05 || maxVal > 1.05
	}
	mask := image.NewGray(image.Rect(0, 0, width, height))
	for index, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, errors.New("invalid cutout mask")
		}
		p := float64(value)
		if isLogits {
			p = 1 / (1 + math.Exp(-p))
		} else {
			p = max(0, min(1, p))
		}
		mask.Pix[index] = uint8(math.Round(255 * p))
	}
	alpha := image.NewGray(src.Bounds())
	draw.BiLinear.Scale(alpha, alpha.Bounds(), mask, mask.Bounds(), draw.Src, nil)
	result := image.NewNRGBA(src.Bounds())
	draw.Draw(result, result.Bounds(), src, src.Bounds().Min, draw.Src)
	decontaminateForeground(result, alpha)
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			offset := result.PixOffset(x, y) + 3
			result.Pix[offset] = uint8((uint16(result.Pix[offset])*uint16(alpha.GrayAt(x, y).Y) + 127) / 255)
		}
	}
	return result, nil
}

// decontaminateForeground replaces background color spill in semi-transparent
// edge pixels with weighted colors blurred from nearby solid foreground pixels.
func decontaminateForeground(img *image.NRGBA, alpha *image.Gray) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	total := w * h
	wr := make([]float32, total)
	wg := make([]float32, total)
	wb := make([]float32, total)
	wa := make([]float32, total)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			idx := (y-b.Min.Y)*w + (x - b.Min.X)
			off := img.PixOffset(x, y)
			a := float32(alpha.GrayAt(x, y).Y) / 255
			origA := float32(img.Pix[off+3]) / 255
			wt := a * origA
			wt *= wt
			wr[idx] = float32(img.Pix[off+0]) * wt
			wg[idx] = float32(img.Pix[off+1]) * wt
			wb[idx] = float32(img.Pix[off+2]) * wt
			wa[idx] = wt
		}
	}
	tmp := make([]float32, total)
	prefix := make([]float32, max(w, h)+1)
	for _, buf := range [][]float32{wr, wg, wb, wa} {
		boxBlurSeparable(buf, tmp, prefix, w, h, 5)
		boxBlurSeparable(buf, tmp, prefix, w, h, 7)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			idx := (y-b.Min.Y)*w + (x - b.Min.X)
			off := img.PixOffset(x, y)
			a := float32(alpha.GrayAt(x, y).Y) / 255
			if a <= 0.02 {
				img.Pix[off+0], img.Pix[off+1], img.Pix[off+2] = 0, 0, 0
				continue
			}
			if a >= 0.98 || wa[idx] <= 1e-4 {
				continue
			}
			estR := wr[idx] / wa[idx]
			estG := wg[idx] / wa[idx]
			estB := wb[idx] / wa[idx]
			blend := min(float32(1), (1-a)*1.2)
			img.Pix[off+0] = clampByte(float32(img.Pix[off+0])*(1-blend) + estR*blend)
			img.Pix[off+1] = clampByte(float32(img.Pix[off+1])*(1-blend) + estG*blend)
			img.Pix[off+2] = clampByte(float32(img.Pix[off+2])*(1-blend) + estB*blend)
		}
	}
}

func boxBlurSeparable(buf, tmp, prefix []float32, w, h, radius int) {
	for y := range h {
		row := y * w
		prefix[0] = 0
		for x := range w {
			prefix[x+1] = prefix[x] + buf[row+x]
		}
		for x := range w {
			lo, hi := max(0, x-radius), min(w-1, x+radius)
			tmp[row+x] = (prefix[hi+1] - prefix[lo]) / float32(hi-lo+1)
		}
	}
	for x := range w {
		prefix[0] = 0
		for y := range h {
			prefix[y+1] = prefix[y] + tmp[y*w+x]
		}
		for y := range h {
			lo, hi := max(0, y-radius), min(h-1, y+radius)
			buf[y*w+x] = (prefix[hi+1] - prefix[lo]) / float32(hi-lo+1)
		}
	}
}

func clampByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(float64(v)))
}
