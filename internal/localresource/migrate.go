package localresource

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// MigrationItem describes a single known resource file eligible for directory migration.
type MigrationItem struct {
	RelPath string
	Size    int64
	SHA     string
	Comfy   bool
}

// SameDirectory reports whether a and b refer to the same directory path.
func SameDirectory(a, b string) bool {
	ca := filepath.Clean(strings.TrimSpace(a))
	cb := filepath.Clean(strings.TrimSpace(b))
	if ca == "" || cb == "" {
		return ca == cb
	}
	if ia, errA := os.Stat(ca); errA == nil {
		if ib, errB := os.Stat(cb); errB == nil {
			return os.SameFile(ia, ib)
		}
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// CollectMigratableFiles scans root for known single-file optional resources and
// ComfyUI model pack files whose size matches the catalog specification.
// Unrelated files, directories, and symlinks are never included.
func CollectMigratableFiles(root string) ([]MigrationItem, int64, bool) {
	if !filepath.IsAbs(root) {
		return nil, 0, false
	}
	var candidates []MigrationItem
	seen := map[string]bool{}
	addCandidate := func(rel string, size int64, sha string, comfy bool) {
		rel = filepath.FromSlash(strings.TrimSpace(rel))
		if rel == "" || !filepath.IsLocal(rel) || seen[rel] {
			return
		}
		seen[rel] = true
		candidates = append(candidates, MigrationItem{
			RelPath: rel,
			Size:    size,
			SHA:     sha,
			Comfy:   comfy,
		})
	}
	for _, m := range Models {
		addCandidate(m.File, m.Size, m.SHA, false)
	}
	if rt, ok := Runtime(); ok {
		addCandidate(rt.File, rt.Size, rt.SHA, false)
	}
	if up, ok := UpscaleRuntime(); ok {
		addCandidate(up.File, up.Size, up.SHA, false)
	}
	for _, pack := range ComfyPacks {
		for _, pf := range pack.ResourceFiles() {
			addCandidate(pf.RelPath, pf.Size, pf.SHA, true)
		}
	}

	var items []MigrationItem
	var total int64
	var hasComfy bool
	for _, cand := range candidates {
		info, err := os.Lstat(filepath.Join(root, cand.RelPath))
		if err != nil || !info.Mode().IsRegular() || info.Size() != cand.Size {
			continue
		}
		items = append(items, cand)
		total += cand.Size
		if cand.Comfy {
			hasComfy = true
		}
	}
	if !hasComfy {
		if info, err := os.Lstat(filepath.Join(root, "comfyui", "extra_model_paths.yaml")); err == nil && info.Mode().IsRegular() {
			hasComfy = true
		}
	}
	return items, total, hasComfy
}

// Migrate moves all known optional resources from fromDir to toDir.
// Same-volume moves use os.Rename; cross-volume moves stream-copy with SHA-256
// verification and atomic publish before removing the source file.
// Unrelated files in fromDir are left untouched.
func Migrate(ctx context.Context, fromDir, toDir string, progress func(done, total int64)) error {
	if !filepath.IsAbs(fromDir) || !filepath.IsAbs(toDir) {
		return errors.New("invalid resource directory")
	}
	if SameDirectory(fromDir, toDir) {
		return nil
	}
	items, total, hasComfy := CollectMigratableFiles(fromDir)
	if len(items) == 0 && !hasComfy {
		return nil
	}
	return migrateItems(ctx, fromDir, toDir, items, total, hasComfy, progress)
}

func migrateItems(ctx context.Context, fromDir, toDir string, items []MigrationItem, total int64, hasComfy bool, progress func(done, total int64)) error {
	if progress != nil {
		progress(0, total)
	}
	var done int64
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := filepath.Join(fromDir, item.RelPath)
		dst := filepath.Join(toDir, item.RelPath)
		if info, err := os.Lstat(dst); err == nil {
			if !info.Mode().IsRegular() || info.Size() != item.Size {
				return fmt.Errorf("destination file conflict: %s", item.RelPath)
			}
			same, err := filesMatchSHA(src, dst, item.Size, item.SHA)
			if err != nil {
				return err
			}
			if !same {
				return fmt.Errorf("destination file conflict: %s", item.RelPath)
			}
			if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
				return err
			}
			done += item.Size
			if progress != nil {
				progress(done, total)
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err == nil {
			done += item.Size
			if progress != nil {
				progress(done, total)
			}
			continue
		}
		if err := moveFileCopy(ctx, src, dst, item.Size, item.SHA, func(copied int64) {
			if progress != nil {
				progress(done+copied, total)
			}
		}); err != nil {
			return err
		}
		done += item.Size
		if progress != nil {
			progress(done, total)
		}
	}

	if hasComfy {
		if err := restoreMigratedVoicePlugin(fromDir, toDir); err != nil {
			return err
		}
		if err := restoreMigratedGGUFPlugin(fromDir, toDir); err != nil {
			return err
		}
		if _, err := WriteComfyExtraModelPaths(toDir); err != nil {
			return err
		}
		oldYAML := filepath.Join(fromDir, "comfyui", "extra_model_paths.yaml")
		if err := os.Remove(oldYAML); err != nil && !os.IsNotExist(err) {
			return err
		}
		cleanupEmptyComfyDirs(filepath.Join(fromDir, "comfyui"))
	}
	return nil
}

func filesMatchSHA(src, dst string, size int64, expectedSHA string) (bool, error) {
	dstSHA, err := fileSHA256(dst, size)
	if err != nil {
		return false, err
	}
	if expectedSHA != "" {
		if dstSHA != expectedSHA {
			return false, nil
		}
		srcSHA, err := fileSHA256(src, size)
		if err != nil {
			return false, err
		}
		return srcSHA == expectedSHA, nil
	}
	srcSHA, err := fileSHA256(src, size)
	if err != nil {
		return false, err
	}
	return srcSHA == dstSHA, nil
}

func fileSHA256(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, size+1))
	if err != nil {
		return "", err
	}
	if n != size {
		return "", fmt.Errorf("unexpected file size: %s", filepath.Base(path))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func moveFileCopy(ctx context.Context, src, dst string, size int64, expectedSHA string, progress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	parent := filepath.Dir(dst)
	tmp, err := os.CreateTemp(parent, ".freefolio-migrate-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	hash := sha256.New()
	buf := make([]byte, 256*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		nr, rerr := in.Read(buf)
		if nr > 0 {
			if _, werr := tmp.Write(buf[:nr]); werr != nil {
				return werr
			}
			hash.Write(buf[:nr])
			copied += int64(nr)
			if copied > size {
				return fmt.Errorf("unexpected resource size: %s", filepath.Base(src))
			}
			if progress != nil {
				progress(copied)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if copied != size {
		return fmt.Errorf("unexpected resource size: %s", filepath.Base(src))
	}
	if expectedSHA != "" && fmt.Sprintf("%x", hash.Sum(nil)) != expectedSHA {
		return fmt.Errorf("SHA-256 mismatch: %s", filepath.Base(src))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := publishDownload(tmpName, dst); err != nil {
		return err
	}
	cleanupTmp = false
	in.Close()
	return os.Remove(src)
}

func cleanupEmptyComfyDirs(comfyRoot string) {
	info, err := os.Lstat(comfyRoot)
	if err != nil || !info.IsDir() {
		return
	}
	var dirs []string
	_ = filepath.WalkDir(comfyRoot, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
}
