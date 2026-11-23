package chatgpt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/fsutil"
)

const storeName = "chatgpt-connections"

type savedState struct {
	Version  int                `json:"version"`
	HostID   string             `json:"hostID"`
	Profiles map[string]profile `json:"profiles"`
}

// withState holds an OS lock through read, token rotation and atomic replace.
// The lock is automatically released on process exit; no stale-lock deletion.
func (c *Client) withState(ctx context.Context, fn func(*savedState) error) error {
	if c.dir == "" {
		return failure("storage_unavailable")
	}
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return failure("storage_unavailable")
	}
	if err := os.Chmod(c.dir, 0700); err != nil {
		return failure("storage_unavailable")
	}
	path := filepath.Join(c.dir, storeName+".lock")
	if err := regularFile(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return failure("storage_unavailable")
	}
	defer f.Close()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		locked, e := tryLock(f)
		if e != nil {
			return failure("storage_unavailable")
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	defer unlock(f)
	s, err := c.load()
	if err != nil {
		return err
	}
	return fn(s)
}

func regularFile(path string) error {
	i, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !i.Mode().IsRegular() {
		return failure("storage_unavailable")
	}
	return nil
}

func (c *Client) load() (*savedState, error) {
	path := filepath.Join(c.dir, storeName)
	if err := regularFile(path); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return &savedState{Version: 1, Profiles: map[string]profile{}}, nil
	}
	if err != nil || info.Size() > 4<<20 {
		return nil, failure("storage_invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, failure("storage_unavailable")
	}
	var envelope struct {
		Version int    `json:"version"`
		Data    []byte `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Version != 1 || len(envelope.Data) == 0 {
		return nil, failure("storage_invalid")
	}
	plain, err := c.crypt(envelope.Data, false)
	if err != nil {
		return nil, failure("encryption_unavailable")
	}
	var s savedState
	if json.Unmarshal(plain, &s) != nil || s.Version != 1 || s.Profiles == nil || !strings.HasPrefix(s.HostID, "urn:uuid:") {
		return nil, failure("storage_invalid")
	}
	for id, p := range s.Profiles {
		if id == "" || !validClientID(p.ClientID) || (p.Status != Connected && p.Status != Disconnected && p.Status != ReauthRequired) || p.Status == Connected && (p.Subject == "" || p.Tokens == nil) {
			return nil, failure("storage_invalid")
		}
	}
	return &s, nil
}

func (c *Client) save(s *savedState) error {
	if s.HostID == "" {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			return err
		}
		value[6] = value[6]&15 | 64
		value[8] = value[8]&63 | 128
		h := hex.EncodeToString(value[:])
		s.HostID = "urn:uuid:" + h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	}
	plain, err := json.Marshal(s)
	if err != nil {
		return failure("storage_invalid")
	}
	sealed, err := c.crypt(plain, true)
	if err != nil {
		return failure("encryption_unavailable")
	}
	data, err := json.Marshal(struct {
		Version int    `json:"version"`
		Data    []byte `json:"data"`
	}{1, sealed})
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, storeName)
	if err := regularFile(path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure("storage_unavailable")
	}
	if err := fsutil.WriteFileAtomic(path, data, 0600); err != nil {
		return failure("storage_unavailable")
	}
	return nil
}
