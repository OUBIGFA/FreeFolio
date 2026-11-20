package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	maxFileBytes          int64 = 64 << 20
	remoteListConcurrency       = 4
)

var (
	errRemoteChanged     = errors.New("远端文件已变化，请重新同步")
	errRemoteMissing     = errors.New("远端文件不存在")
	errRemoteNotModified = errors.New("remote content not modified")
)

// Conditional writes combine FreeType's transports with ArcMD's incremental
// synchronization semantics. An absent ETag never becomes an unsafe overwrite.
func remoteRequest(s remoteStore, method, key string, data []byte, conditions map[string]string) (*http.Response, error) {
	if !safeRelative(key) {
		return nil, errors.New("invalid remote path")
	}
	switch v := s.(type) {
	case *webdavStore:
		if method == http.MethodPut {
			parts := cleanKey(key)
			if err := v.ensureDirs(parts[:len(parts)-1]); err != nil {
				return nil, err
			}
		}
		var body io.Reader
		if data != nil {
			body = bytes.NewReader(data)
		}
		return v.do(method, v.address(cleanKey(key), false), body, int64(len(data)), conditions)
	case *s3Store:
		var body io.ReadSeeker
		if data != nil {
			body = bytes.NewReader(data)
		}
		lower := map[string]string{}
		for k, value := range conditions {
			lower[strings.ToLower(k)] = value
		}
		return v.do(method, v.object(key), nil, body, int64(len(data)), lower)
	default:
		return nil, errors.New("unsupported sync transport")
	}
}

func readRemote(s remoteStore, key, validator string) ([]byte, string, error) {
	conditions := map[string]string{}
	if validator != "" {
		conditions["If-None-Match"] = validator
	}
	resp, err := remoteRequest(s, http.MethodGet, key, nil, conditions)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && validator != "" {
		return nil, validator, errRemoteNotModified
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", errRemoteMissing
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", remoteError("下载", resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes*2+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxFileBytes*2 {
		return nil, "", errors.New("远端文件超过同步大小限制")
	}
	return data, resp.Header.Get("ETag"), nil
}

func writeRemote(s remoteStore, key string, data []byte, etag string, exists bool) (string, error) {
	cond := map[string]string{"Content-Type": "application/octet-stream"}
	if exists {
		if etag == "" || strings.HasPrefix(etag, "W/") {
			return "", errors.New("服务未提供可用于安全覆盖的 ETag")
		}
		cond["If-Match"] = etag
	} else {
		cond["If-None-Match"] = "*"
	}
	resp, err := remoteRequest(s, http.MethodPut, key, data, cond)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusConflict {
		return "", errRemoteChanged
	}
	if resp.StatusCode/100 != 2 {
		return "", remoteError("上传", resp)
	}
	// Only the upload response binds this validator to the bytes we sent.
	// A subsequent HEAD could instead describe a concurrent remote edit.
	return resp.Header.Get("ETag"), nil
}

func deleteRemote(s remoteStore, key, etag string) error {
	if etag == "" || strings.HasPrefix(etag, "W/") {
		return errors.New("服务未提供可用于安全删除的 ETag")
	}
	resp, err := remoteRequest(s, http.MethodDelete, key, nil, map[string]string{"If-Match": etag})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPreconditionFailed {
		return errRemoteChanged
	}
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return remoteError("删除", resp)
	}
	return nil
}

// Remote inventory adapts rclone's ListR / bounded directory walk strategy.
// It rejects incomplete results before the sync engine can apply any changes.
func listRemote(s remoteStore, root string, progress func(string)) (map[string]remoteEntry, error) {
	out := map[string]remoteEntry{}
	seen := map[string]bool{}
	add := func(rel string, e remoteEntry) error {
		if !safeRelative(rel) {
			return fmt.Errorf("invalid remote name %q", rel)
		}
		depth := strings.Count(rel, "/")
		if e.Dir {
			depth++
		}
		if depth > 64 || len(seen) >= 100000 {
			return errors.New("远端目录层级或数量超过限制")
		}
		if seen[rel] {
			return errors.New("远端目录返回重复路径")
		}
		seen[rel] = true
		if excluded(rel) || e.Dir {
			return nil
		}
		if e.Size < 0 || e.Size > maxFileBytes*2 {
			return fmt.Errorf("文件过大：%s", rel)
		}
		out[rel] = e
		return nil
	}
	if bucket, ok := s.(*s3Store); ok {
		if progress != nil {
			progress(root)
		}
		entries, err := bucket.list(root, 0, true)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if err := add(e.Name, e); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	dav, ok := s.(*webdavStore)
	if !ok {
		return nil, errors.New("unsupported sync transport")
	}
	// The scan owns cancellation, so a failed directory stops its peers without
	// canceling the caller's context. Progress and map updates stay serial.
	ctx, cancel := context.WithCancelCause(dav.ctx)
	defer cancel(nil)
	scan := *dav
	scan.ctx = ctx
	pending := []string{root}
	for len(pending) > 0 {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		batch := pending[:min(remoteListConcurrency, len(pending))]
		pending = pending[len(batch):]
		results := make([][]remoteEntry, len(batch))
		var workers sync.WaitGroup
		for i, dir := range batch {
			if progress != nil {
				progress(dir)
			}
			workers.Go(func() {
				entries, err := scan.List(dir)
				if err != nil {
					cancel(err)
					return
				}
				results[i] = entries
			})
		}
		workers.Wait()
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		for i, entries := range results {
			for _, e := range entries {
				if !safeRelative(e.Name) || strings.Contains(e.Name, "/") {
					return nil, fmt.Errorf("invalid remote name %q", e.Name)
				}
				key := batch[i] + "/" + e.Name
				rel := strings.TrimPrefix(key, root+"/")
				if err := add(rel, e); err != nil {
					return nil, err
				}
				if e.Dir && !excluded(rel) {
					pending = append(pending, key)
				}
			}
		}
	}
	return out, nil
}
