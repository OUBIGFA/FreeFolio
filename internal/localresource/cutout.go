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

const side = 1024
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
	input, err := ort.NewTensor([]int64{1, 3, side, side}, pixels(original))
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
	result, err := applyMask(original, mask, side, side)
	if err != nil {
		return err
	}
	return png.Encode(stdout, result)
}

func pixels(src image.Image) []float32 {
	resized := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(resized, resized.Bounds(), image.White, image.Point{}, draw.Src)
	draw.BiLinear.Scale(resized, resized.Bounds(), src, src.Bounds(), draw.Over, nil)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	out := make([]float32, 3*side*side)
	for index := range side * side {
		for channel := range 3 {
			out[channel*side*side+index] = (float32(resized.Pix[index*4+channel])/255 - mean[channel]) / std[channel]
		}
	}
	return out
}

func applyMask(src image.Image, logits []float32, width, height int) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 || len(logits) != width*height {
		return nil, errors.New("unexpected cutout mask size")
	}
	mask := image.NewGray(image.Rect(0, 0, width, height))
	for index, value := range logits {
		if math.IsNaN(float64(value)) {
			return nil, errors.New("invalid cutout mask")
		}
		mask.Pix[index] = uint8(math.Round(255 / (1 + math.Exp(-float64(value)))))
	}
	alpha := image.NewGray(src.Bounds())
	draw.BiLinear.Scale(alpha, alpha.Bounds(), mask, mask.Bounds(), draw.Src, nil)
	result := image.NewNRGBA(src.Bounds())
	draw.Draw(result, result.Bounds(), src, src.Bounds().Min, draw.Src)
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			offset := result.PixOffset(x, y) + 3
			result.Pix[offset] = uint8((uint16(result.Pix[offset])*uint16(alpha.GrayAt(x, y).Y) + 127) / 255)
		}
	}
	return result, nil
}
