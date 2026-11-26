package localresource

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Resource struct {
	ID, Name, File, URL, SHA string
	Size                     int64
	member                   string
}

var Models = []Resource{
	{ID: "birefnet-lite", Name: "BiRefNet Lite", File: "birefnet-lite-fp16.onnx", URL: "https://huggingface.co/onnx-community/BiRefNet_lite-ONNX/resolve/de15b22ba131738a16dff04aab8bdf8dc32e3ac1/onnx/model_fp16.onnx", SHA: "d39b897ceb16ae654c1731f3dba0cf9b368d9cae74b5a57459b455cc8bfec402", Size: 114538221},
	{ID: "birefnet-general", Name: "BiRefNet General", File: "birefnet-general-fp16.onnx", URL: "https://huggingface.co/onnx-community/BiRefNet-ONNX/resolve/534d3c82d3bb8b2f0867db6dfbc3a525b8e42f67/onnx/model_fp16.onnx", SHA: "3654c741eb80bd926ada8fed1713b506ccf8d30eb1f6487e87eb9f234f33df09", Size: 489666272},
}

func Model(id string) (Resource, bool) {
	for _, model := range Models {
		if model.ID == id {
			return model, true
		}
	}
	return Resource{}, false
}

func Runtime() (Resource, bool) {
	const prefix = "https://files.pythonhosted.org/packages/"
	item := Resource{ID: "onnxruntime", Name: "ONNX Runtime 1.28 CPU"}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		item.File = "onnxruntime-1.28.0-cp311-cp311-win_amd64.whl"
		item.URL = prefix + "9f/10/3d946d5d5f2cdcc3c8da36cae63190c516d16349edaffd944bda60ca4c3e/" + item.File
		item.SHA, item.Size, item.member = "0d650aeee29368414367b65529e90afe4bf1bab76254789063b8b2f7ea3013c8", 13752539, "onnxruntime/capi/onnxruntime.dll"
	case "linux/amd64":
		item.File = "onnxruntime-1.28.0-cp311-cp311-manylinux_2_27_x86_64.manylinux_2_28_x86_64.whl"
		item.URL = prefix + "f3/17/4e5ecd8764f87573c495d834ce79e61ecca47f7a01d1e444a606e570edcb/" + item.File
		item.SHA, item.Size, item.member = "a166b78ee04f3a37fa1ef82034b6a3ce96d9684e582d4d30b296de83e9998bb5", 19193162, "onnxruntime/capi/libonnxruntime.so.1.28.0"
	case "darwin/arm64":
		item.File = "onnxruntime-1.28.0-cp311-cp311-macosx_14_0_arm64.whl"
		item.URL = prefix + "17/4d/5014667e2a3a77d6e1b74cc3d88948d06163b8e0a33a84c85073322b5dec/" + item.File
		item.SHA, item.Size, item.member = "f5c5daabd28aad610f83fdcf32acec8fb57e6adc6c6a39fe2a3c755db957b410", 19130506, "onnxruntime/capi/libonnxruntime.1.28.0.dylib"
	default:
		return Resource{}, false
	}
	return item, true
}

func Present(root string, item Resource) bool {
	info, err := os.Lstat(filepath.Join(root, item.File))
	return err == nil && info.Mode().IsRegular() && info.Size() == item.Size
}

func verified(root string, item Resource) (*os.File, error) {
	name := filepath.Join(root, item.File)
	if !filepath.IsAbs(root) || !filepath.IsLocal(item.File) || filepath.Base(item.File) != item.File {
		return nil, errors.New("invalid resource path")
	}
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != item.Size {
		return nil, fmt.Errorf("unexpected resource file: %s", item.File)
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, item.Size+1))
	if err == nil && (written != item.Size || fmt.Sprintf("%x", hash.Sum(nil)) != item.SHA) {
		err = fmt.Errorf("SHA-256 mismatch: %s", item.File)
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func Remove(root string, item Resource, trash func(string) error) error {
	file, err := verified(root, item)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return trash(filepath.Join(root, item.File))
}

func Install(ctx context.Context, root string, item Resource, progress func(int64)) error {
	client := &http.Client{Timeout: 45 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
			return errors.New("unsafe resource redirect")
		}
		return nil
	}}
	return install(ctx, root, item, client, progress)
}

type progressWriter func(int64)

func (report progressWriter) Write(data []byte) (int, error) {
	report(int64(len(data)))
	return len(data), nil
}

func install(ctx context.Context, root string, item Resource, client *http.Client, progress func(int64)) error {
	if !filepath.IsAbs(root) || !filepath.IsLocal(item.File) || filepath.Base(item.File) != item.File {
		return errors.New("invalid resource path")
	}
	if existing, err := verified(root, item); err == nil {
		return existing.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return err
	}
	if req.URL.Scheme != "https" {
		return errors.New("resources require HTTPS")
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", item.Name, res.StatusCode)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".freefolio-resource-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	writers := []io.Writer{tmp, hash}
	if progress != nil {
		var received int64
		writers = append(writers, progressWriter(func(n int64) { received += n; progress(received) }))
	}
	n, err := io.Copy(io.MultiWriter(writers...), io.LimitReader(res.Body, item.Size+1))
	if err != nil {
		return err
	}
	if n != item.Size || fmt.Sprintf("%x", hash.Sum(nil)) != item.SHA {
		return fmt.Errorf("size or SHA-256 mismatch: %s", item.File)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return publishDownload(tmp.Name(), filepath.Join(root, item.File))
}
