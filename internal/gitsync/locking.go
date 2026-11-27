package gitsync

import (
	"context"
	"errors"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"os"
	"path/filepath"
)

// claimIndex respects native Git's lock protocol. A prepared index is published
// only after a compare-and-swap HEAD update; unrelated staged entries survive.
func (g *Git) claimIndex(ctx context.Context, root string) (string, string, func(), error) {
	index, e := g.text(ctx, root, "rev-parse", "--git-path", "index")
	if e != nil {
		return "", "", nil, e
	}
	if !filepath.IsAbs(index) {
		index = filepath.Join(root, index)
	}
	lock := index + ".lock"
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", "", nil, errors.New(i18n.T("另一个 Git 操作正在执行，请稍后重试"))
	}
	f.Close()
	cleanup := func() { os.Remove(lock) }
	return index, lock, cleanup, nil
}
func (g *Git) operationLock(ctx context.Context, root string) (func(), error) {
	dir := g.mergeDir(root)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	ok, e := tryLock(f)
	if e != nil || !ok {
		f.Close()
		return nil, errors.New(i18n.T("此仓库正在由另一个 FreeFolio 任务处理"))
	}
	return func() { unlock(f); f.Close() }, nil
}
