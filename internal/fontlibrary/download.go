// Package fontlibrary owns verified optional font files outside document spaces.
package fontlibrary

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

type File struct {
	Name, URL, Member, SHA256 string
	Bytes                     int64
	Zlib                      bool
}
type Font struct {
	ID, Family, Name, Group, LicenseURL string
	Files                               []File
}

// Catalog pins the exact font bytes reviewed with Quiet.md dfdf6da. The
// publishers' versioned release URLs are preferred to a moving font service.
var Catalog = []Font{
	{ID: "wenkai", Family: "LXGW WenKai", Name: "霞鹜文楷", Group: "楷体与手写", LicenseURL: "https://github.com/lxgw/LxgwWenKai/blob/v1.530/OFL.txt", Files: []File{
		{Name: "wenkai-regular.ttf", URL: "https://github.com/lxgw/LxgwWenKai/releases/download/v1.530/LXGWWenKai-Regular.ttf", SHA256: "2d73eea94ad808999d90c0000c239a84a33b09d4bc6ab6c5b8bbf1eda8a7fc8b", Bytes: 25928940},
		{Name: "wenkai-medium.ttf", URL: "https://github.com/lxgw/LxgwWenKai/releases/download/v1.530/LXGWWenKai-Medium.ttf", SHA256: "f4b8a34d1f838fe411540cb7c97b1c384ac9738a876c4104500bc42508b2312a", Bytes: 25800076},
	}},
	{ID: "zhuque", Family: "Zhuque Fangsong (technical preview)", Name: "朱雀仿宋", Group: "宋体与衬线", LicenseURL: "https://github.com/TrionesType/zhuque/blob/v0.212/LICENSE.txt", Files: []File{
		{Name: "zhuque.ttf", URL: "https://github.com/TrionesType/zhuque/releases/download/v0.212/ZhuqueFangsong-v0.212.zip", Member: "ZhuqueFangsong-Regular.ttf", SHA256: "558c62730844fe54ba220146ed62f859d4e2880188d92d985f8921c6e3743bc4", Bytes: 8824084},
	}},
	{ID: "xiaolai", Family: "Xiaolai", Name: "小赖手写体", Group: "楷体与手写", LicenseURL: "https://github.com/lxgw/kose-font/blob/master/OFL.txt", Files: []File{
		{Name: "xiaolai.ttf.zlib", URL: "https://raw.githubusercontent.com/s1dashu/quiet-md/dfdf6da369eb0432a7f6c9024ca4f5c7605de4da/assets/fonts/excalidraw/Xiaolai-Regular.ttf.zlib", SHA256: "630ab9f0e12f6115c65af0b1681b09321769178e0847254457ca91a159049dc1", Bytes: 14232970, Zlib: true},
	}},
}

func validFile(f File) bool {
	return f.Name != "" && filepath.Base(f.Name) == f.Name && !strings.ContainsAny(f.Name, "/\\:") && len(f.SHA256) == 64 && f.Bytes > 0 && f.Bytes <= 64<<20
}
func verify(data []byte, f File) error {
	if int64(len(data)) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != f.SHA256 {
		return errors.New("font checksum or size mismatch")
	}
	return nil
}

// Read checks a cache entry on every load, and expands only verified bytes.
func Read(dir string, f File) ([]byte, error) {
	if !validFile(f) {
		return nil, errors.New("invalid font entry")
	}
	file := filepath.Join(dir, f.Name)
	info, e := os.Lstat(file)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() != f.Bytes {
		return nil, errors.New("invalid font cache file")
	}
	data, e := os.ReadFile(file)
	if e != nil {
		return nil, e
	}
	if e = verify(data, f); e != nil {
		return nil, e
	}
	if !f.Zlib {
		return data, nil
	}
	r, e := zlib.NewReader(bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	defer r.Close()
	data, e = io.ReadAll(io.LimitReader(r, 64<<20+1))
	if len(data) > 64<<20 {
		return nil, errors.New("expanded font too large")
	}
	return data, e
}

// Download never replaces a cache entry until both size and SHA-256 match.
// The progress callback reports received bytes and the advertised total.
func Download(ctx context.Context, client *http.Client, dir string, files []File, progress func(int64, int64)) error {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many font redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("font redirect requires HTTPS")
		}
		return nil
	}
	var total, done int64
	for _, f := range files {
		total += f.Bytes
	}
	for _, f := range files {
		if !validFile(f) {
			return errors.New("invalid font entry")
		}
		u, e := url.Parse(f.URL)
		if e != nil || u.Scheme != "https" || u.User != nil {
			return errors.New("font download requires HTTPS")
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
		if e != nil {
			return e
		}
		res, e := copyClient.Do(req)
		if e != nil {
			return e
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return fmt.Errorf("font download: HTTP %d", res.StatusCode)
		}
		var buf bytes.Buffer
		chunk := make([]byte, 64<<10)
		for {
			n, readErr := res.Body.Read(chunk)
			if n > 0 {
				buf.Write(chunk[:n])
				if buf.Len() > 96<<20 {
					res.Body.Close()
					return errors.New("font download too large")
				}
				if progress != nil {
					progress(done+min(int64(buf.Len()), f.Bytes), total)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				res.Body.Close()
				return readErr
			}
		}
		res.Body.Close()
		data := buf.Bytes()
		if f.Member != "" {
			zr, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if e != nil {
				return e
			}
			var found bool
			for _, entry := range zr.File {
				if filepath.Base(entry.Name) != f.Member {
					continue
				}
				if found || entry.Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 > 64<<20 {
					return errors.New("invalid font archive member")
				}
				rc, e := entry.Open()
				if e != nil {
					return e
				}
				data, e = io.ReadAll(io.LimitReader(rc, 64<<20+1))
				rc.Close()
				if e != nil {
					return e
				}
				found = true
			}
			if !found {
				return errors.New("font archive member missing")
			}
		}
		if e = verify(data, f); e != nil {
			return e
		}
		if e = fsutil.WriteFileAtomic(filepath.Join(dir, f.Name), data, 0600); e != nil {
			return e
		}
		done += f.Bytes
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}
