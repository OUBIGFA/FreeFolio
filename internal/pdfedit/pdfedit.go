// Package pdfedit changes PDF files page by page: rotating, removing and
// extracting pages, merging, splitting, and adding or removing a password.
// It works on bytes and never touches the file system itself, so callers
// decide how results are written (atomically, after checking for outside
// changes). It has no dependency on the window or drawing toolkit.
//
// pdfcpu runs with its stateless configuration: it reads and writes no
// configuration directory of its own.
package pdfedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// MaxSize bounds the PDFs edited in memory.
const MaxSize = 256 << 20

// ErrTooLarge reports a PDF beyond MaxSize.
var ErrTooLarge = errors.New("pdfedit: the PDF is too large to edit")

// ErrNoPages reports a page selection that selects nothing.
var ErrNoPages = errors.New("pdfedit: no pages selected")

// ErrAllPages reports removing every page of a document.
var ErrAllPages = errors.New("pdfedit: a PDF must keep at least one page")

func config(password string) *model.Configuration {
	conf := model.NewStatelessConfiguration()
	if password != "" {
		conf.UserPW, conf.OwnerPW = password, password
	}
	return conf
}

func check(data []byte) error {
	if len(data) > MaxSize {
		return ErrTooLarge
	}
	if len(data) < 5 || !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return errors.New("pdfedit: not a PDF file")
	}
	return nil
}

// wrap turns pdfcpu's errors and panics on malformed files into errors.
func wrap(op string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdfedit: %s: malformed PDF (%v)", op, r)
		}
	}()
	if err = fn(); err != nil {
		return fmt.Errorf("pdfedit: %s: %w", op, err)
	}
	return nil
}

// PageCount returns how many pages data has.
func PageCount(ctx context.Context, data []byte, password string) (int, error) {
	if err := check(data); err != nil {
		return 0, err
	}
	var n int
	err := wrap("page count", func() (err error) {
		n, err = api.PageCount(ctx, bytes.NewReader(data), config(password))
		return err
	})
	return n, err
}

// ParsePages reads a page selection as people type it, "1-3, 5, 8-",
// against a document of count pages, returning sorted unique 1-based page
// numbers. An empty selection is every page.
func ParsePages(sel string, count int) ([]int, error) {
	if count <= 0 {
		return nil, ErrNoPages
	}
	sel = strings.NewReplacer("，", ",", "、", ",", "；", ",", ";", ",", "～", "-", "~", "-", "–", "-", "—", "-", " ", "").Replace(sel)
	if sel == "" {
		all := make([]int, count)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}
	seen := map[int]bool{}
	for part := range strings.SplitSeq(sel, ",") {
		if part == "" {
			continue
		}
		lo, hi := 0, 0
		if a, b, ok := strings.Cut(part, "-"); ok {
			var err error
			if lo, err = pageNumber(a, 1); err != nil {
				return nil, err
			}
			if hi, err = pageNumber(b, count); err != nil {
				return nil, err
			}
		} else {
			n, err := pageNumber(part, 0)
			if err != nil {
				return nil, err
			}
			lo, hi = n, n
		}
		if lo < 1 || hi > count || lo > hi {
			return nil, fmt.Errorf("pdfedit: pages %q are outside 1-%d", part, count)
		}
		for p := lo; p <= hi; p++ {
			seen[p] = true
		}
	}
	var out []int
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	if len(out) == 0 {
		return nil, ErrNoPages
	}
	return out, nil
}

func pageNumber(s string, empty int) (int, error) {
	if s == "" {
		if empty == 0 {
			return 0, ErrNoPages
		}
		return empty, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pdfedit: %q is not a page number", s)
	}
	return n, nil
}

func selection(pages []int) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = strconv.Itoa(p)
	}
	return out
}

// Rotate turns the pages clockwise by degrees, a multiple of 90.
func Rotate(ctx context.Context, data []byte, pages []int, degrees int, password string) ([]byte, error) {
	if err := check(data); err != nil {
		return nil, err
	}
	if degrees%90 != 0 {
		return nil, errors.New("pdfedit: rotation must be a multiple of 90 degrees")
	}
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	var out bytes.Buffer
	err := wrap("rotate", func() error {
		return api.Rotate(ctx, bytes.NewReader(data), &out, degrees, selection(pages), config(password))
	})
	return out.Bytes(), err
}

// Remove deletes the pages; at least one page must remain.
func Remove(ctx context.Context, data []byte, pages []int, password string) ([]byte, error) {
	n, err := PageCount(ctx, data, password)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	if len(pages) >= n {
		return nil, ErrAllPages
	}
	var out bytes.Buffer
	err = wrap("remove pages", func() error {
		return api.RemovePages(ctx, bytes.NewReader(data), &out, selection(pages), config(password))
	})
	return out.Bytes(), err
}

// Extract returns a new PDF of the pages, in document order.
func Extract(ctx context.Context, data []byte, pages []int, password string) ([]byte, error) {
	n, err := PageCount(ctx, data, password)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	if len(pages) == n {
		return data, nil
	}
	// Removing the others keeps outlines and resources pdfcpu can keep.
	keep := map[int]bool{}
	for _, p := range pages {
		keep[p] = true
	}
	var drop []int
	for p := 1; p <= n; p++ {
		if !keep[p] {
			drop = append(drop, p)
		}
	}
	return Remove(ctx, data, drop, password)
}

// Merge joins documents in order into one.
func Merge(ctx context.Context, docs [][]byte) ([]byte, error) {
	if len(docs) < 2 {
		return nil, errors.New("pdfedit: merging needs at least two PDFs")
	}
	total := 0
	rs := make([]io.ReadSeeker, len(docs))
	for i, d := range docs {
		if err := check(d); err != nil {
			return nil, err
		}
		if total += len(d); total > MaxSize {
			return nil, ErrTooLarge
		}
		rs[i] = bytes.NewReader(d)
	}
	var out bytes.Buffer
	err := wrap("merge", func() error {
		return api.MergeRaw(ctx, rs, &out, false, config(""))
	})
	return out.Bytes(), err
}

// Part is a piece of a split document.
type Part struct {
	From, Thru int // 1-based, inclusive
	Data       []byte
}

// Split cuts the document into parts of span pages each (span 1 is one
// file per page).
func Split(ctx context.Context, data []byte, span int, password string) ([]Part, error) {
	if err := check(data); err != nil {
		return nil, err
	}
	if span < 1 {
		return nil, errors.New("pdfedit: pages per part must be at least 1")
	}
	var parts []Part
	err := wrap("split", func() error {
		spans, err := api.SplitRaw(ctx, bytes.NewReader(data), span, config(password))
		if err != nil {
			return err
		}
		for _, s := range spans {
			b, err := io.ReadAll(s.Reader)
			if err != nil {
				return err
			}
			parts = append(parts, Part{From: s.From, Thru: s.Thru, Data: b})
		}
		return nil
	})
	return parts, err
}

// Encrypted reports whether data asks for a password.
func Encrypted(ctx context.Context, data []byte) bool {
	if check(data) != nil {
		return false
	}
	_, err := PageCount(ctx, data, "")
	return err != nil && isPasswordError(err)
}

func isPasswordError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "password") || strings.Contains(s, "encrypt")
}

// Encrypt protects the document with password, AES-256.
func Encrypt(ctx context.Context, data []byte, password string) ([]byte, error) {
	if err := check(data); err != nil {
		return nil, err
	}
	if len(password) == 0 {
		return nil, errors.New("pdfedit: the password is empty")
	}
	conf := model.NewStatelessConfiguration()
	conf.UserPW, conf.OwnerPW = password, password
	conf.EncryptUsingAES, conf.EncryptKeyLength = true, 256
	var out bytes.Buffer
	err := wrap("encrypt", func() error {
		return api.Encrypt(ctx, bytes.NewReader(data), &out, conf)
	})
	return out.Bytes(), err
}

// Decrypt removes the password of the document.
func Decrypt(ctx context.Context, data []byte, password string) ([]byte, error) {
	if err := check(data); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err := wrap("decrypt", func() error {
		return api.Decrypt(ctx, bytes.NewReader(data), &out, config(password))
	})
	return out.Bytes(), err
}
