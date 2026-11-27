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
	"strconv"
	"strings"
)

// downloadFile retains a catalog-specific partial file on interruption. It
// publishes only complete, verified bytes, never replacing an existing file.
func downloadFile(ctx context.Context, target, source string, size int64, digest string, client *http.Client, progress func(int64)) error {
	if size <= 0 {
		return errors.New("invalid download size")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := sha256.Sum256([]byte(source + "\x00" + digest + "\x00" + strconv.FormatInt(size, 10)))
	partial := filepath.Join(filepath.Dir(target), ".freefolio-"+fmt.Sprintf("%x", key[:12])+".part")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(partial); err == nil && !info.Mode().IsRegular() {
		return errors.New("download partial is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	offset := stat.Size()
	if offset > size || digest == "" && offset > 0 {
		if err = f.Truncate(0); err != nil {
			return err
		}
		offset = 0
	}
	hash := sha256.New()
	if offset > 0 {
		if _, err = io.CopyN(hash, f, offset); err != nil {
			return err
		}
	}
	report := func(done int64) {
		if progress != nil {
			progress(done)
		}
	}
	report(offset)
	if offset < size {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return err
		}
		if req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("resources require HTTPS")
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		switch res.StatusCode {
		case http.StatusOK:
			// A server may ignore Range. Restart only this partial, never append
			// a complete response to it and silently corrupt the model.
			if offset > 0 {
				if err = f.Truncate(0); err != nil {
					return err
				}
				if _, err = f.Seek(0, io.SeekStart); err != nil {
					return err
				}
				offset = 0
				hash.Reset()
				report(0)
			}
		case http.StatusPartialContent:
			var start, end, total int64
			value := res.Header.Get("Content-Range")
			if _, err = fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total); err != nil || !strings.HasPrefix(value, "bytes ") || start != offset || end != size-1 || total != size {
				return errors.New("server returned an invalid resume range")
			}
		default:
			return fmt.Errorf("resource download: HTTP %d", res.StatusCode)
		}
		received := offset
		_, err = io.Copy(io.MultiWriter(f, hash, progressWriter(func(n int64) { received += n; report(min(received, size)) })), io.LimitReader(res.Body, size-offset+1))
		if syncErr := f.Sync(); syncErr != nil {
			return syncErr
		}
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		if received != size {
			if received > size {
				if err = f.Truncate(0); err != nil {
					return err
				}
			}
			return errors.New("resource download is incomplete or exceeds its expected size")
		}
	}
	if digest != "" && fmt.Sprintf("%x", hash.Sum(nil)) != digest {
		// An owned partial with invalid bytes cannot be resumed safely.
		if err = f.Truncate(0); err != nil {
			return err
		}
		return fmt.Errorf("SHA-256 mismatch: %s", filepath.Base(target))
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return publishDownload(partial, target)
}
