package gitsync

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const MinGitVersion = "2.56.1"

var errRuntimeDigest error = translatedError("Git 下载 SHA-256 校验失败")

type RuntimeProgress struct{ Received, Total int64 }

func FindGit(dir string) string {
	if p, e := exec.LookPath("git"); e == nil {
		return p
	}
	p := filepath.Join(dir, "git", MinGitVersion, "cmd", "git.exe")
	if _, e := os.Stat(p); e == nil {
		return p
	}
	return ""
}

// EnsureGit uses the existing installation first. A missing Windows engine is
// downloaded to application resources, never installed globally or on PATH.
func EnsureGit(ctx context.Context, dir string, progress func(RuntimeProgress)) (string, error) {
	if p := FindGit(dir); p != "" {
		return p, nil
	}
	if runtime.GOOS != "windows" {
		return "", errors.New(i18n.T("未找到 Git，请通过系统软件管理器安装 Git"))
	}
	arch, digest := "64-bit", "4a92850b6e917da4fd010669b614b55dccf5b0ec333a534cc4be4700dfefdafd"
	total := int64(39849768)
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
		digest = "96571e442e90523a90e5fd8661b4c4e43bc192910f3c6db938816e6955b4cc8c"
		total = 37983254
	} else if runtime.GOARCH != "amd64" {
		return "", errors.New(i18n.T("此平台没有可用的 MinGit 资源"))
	}

	parent := filepath.Join(dir, "git")
	name := "MinGit-" + MinGitVersion + "-" + arch + ".zip"
	staging, e := runtimeArchive(ctx, parent, name, "https://github.com/git-for-windows/git/releases/download/v2.56.1.windows.1/"+name, digest, total, progress)
	if e != nil {
		return "", e
	}
	p := filepath.Join(staging, "cmd", "git.exe")
	cmd := exec.CommandContext(ctx, p, "--version")
	hideCommand(cmd)
	if e = cmd.Run(); e != nil {
		return "", errors.New(i18n.T("下载的 Git 无法启动"))
	}
	dest := filepath.Join(parent, MinGitVersion)
	if e = os.Rename(staging, dest); e != nil {
		return "", e
	}
	return filepath.Join(dest, "cmd", "git.exe"), nil
}
func runtimeArchive(ctx context.Context, parent, name, address, digest string, total int64, progress func(RuntimeProgress)) (string, error) {
	if e := os.MkdirAll(parent, 0700); e != nil {
		return "", e
	}
	archive := filepath.Join(parent, name+".partial")
	var offset int64
	if i, e := os.Stat(archive); e == nil {
		offset = i.Size()
	}
	if offset > total {
		return "", errors.New(i18n.T("Git 下载文件大小异常，请移走损坏的下载后重试"))
	}
	if offset < total {
		req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
		if e != nil {
			return "", e
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		client := &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 10 {
				return errors.New(i18n.T("Git 下载重定向无效"))
			}
			return nil
		}}
		res, e := client.Do(req)
		if e != nil {
			return "", e
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusOK {
			offset = 0
		} else if res.StatusCode != http.StatusPartialContent {
			return "", fmt.Errorf(i18n.T("Git 下载失败（HTTP %d）"), res.StatusCode)
		}
		if res.StatusCode == http.StatusPartialContent && !strings.HasPrefix(res.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
			return "", errors.New(i18n.T("Git 下载续传位置无效"))
		}
		flags := os.O_CREATE | os.O_WRONLY
		if offset == 0 {
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_APPEND
		}
		f, e := os.OpenFile(archive, flags, 0600)
		if e != nil {
			return "", e
		}
		buf := make([]byte, 64<<10)
		received := offset
		for {
			n, readErr := res.Body.Read(buf)
			if n > 0 {
				if received+int64(n) > total {
					f.Close()
					return "", errors.New(i18n.T("Git 下载超出声明大小"))
				}
				if _, e = f.Write(buf[:n]); e != nil {
					f.Close()
					return "", e
				}
				received += int64(n)
				if progress != nil {
					progress(RuntimeProgress{received, total})
				}
			}
			if readErr != nil {
				f.Close()
				if readErr != io.EOF {
					return "", readErr
				}
				break
			}
		}
		if received != total {
			return "", errors.New(i18n.T("Git 下载尚未完成，重试将继续下载"))
		}
	}
	// Extraction gets a unique staging directory, so interruption cannot publish
	// a half-installed engine and retries cannot overwrite a working installation.
	staging, e := os.MkdirTemp(parent, "unpack-")
	if e != nil {
		return "", e
	}
	if e = extractRuntime(archive, staging, digest); e != nil {
		if errors.Is(e, errRuntimeDigest) {
			// Preserve the damaged download for inspection and let the next
			// explicit retry start clean instead of resuming corrupt bytes.
			if moveErr := os.Rename(archive, archive+".invalid-"+time.Now().UTC().Format("20060102T150405.000000000")); moveErr != nil {
				return "", fmt.Errorf("%w: %v", e, moveErr)
			}
		}
		return "", e
	}
	return staging, nil
}

func extractRuntime(archive, dest, digest string) error {
	f, e := os.Open(archive)
	if e != nil {
		return e
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	f.Close()
	if e != nil {
		return e
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != digest {
		return errRuntimeDigest
	}
	z, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer z.Close()
	var expanded uint64
	for _, entry := range z.File {
		if entry.Mode()&os.ModeSymlink != 0 || strings.ContainsAny(entry.Name, "\\:\x00") {
			return errors.New(i18n.T("Git 压缩包包含不安全路径"))
		}
		name := strings.TrimSuffix(entry.Name, "/")
		path, e := validPath(dest, name)
		if e != nil {
			return e
		}
		expanded += entry.UncompressedSize64
		if expanded > 512<<20 || entry.UncompressedSize64 > 128<<20 {
			return errors.New(i18n.T("Git 解压大小超出限制"))
		}
		if entry.FileInfo().IsDir() {
			if e = os.MkdirAll(path, 0700); e != nil {
				return e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		r, e := entry.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
		if e != nil {
			r.Close()
			return e
		}
		_, e = io.Copy(out, io.LimitReader(r, int64(entry.UncompressedSize64)+1))
		closeErr := out.Close()
		r.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
