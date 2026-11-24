package pdfedit

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/pdf"
)

// samplePDF makes a PDF of n small pages.
func samplePDF(t *testing.T, n int) []byte {
	t.Helper()
	var pages []pdf.Page
	for i := range n {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		img.Set(i%8, 0, color.Black)
		pages = append(pages, pdf.Page{Image: img, Width: 100, Height: 140})
	}
	var buf bytes.Buffer
	if err := pdf.Write(&buf, "sample", pages); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func count(t *testing.T, data []byte, password string) int {
	t.Helper()
	n, err := PageCount(context.Background(), data, password)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	return n
}

func TestParsePages(t *testing.T) {
	cases := map[string][]int{
		"":       {1, 2, 3, 4, 5},
		"1-3, 5": {1, 2, 3, 5},
		"4-":     {4, 5},
		"-2":     {1, 2},
		"2，2、3":  {2, 3},
		"1～2":    {1, 2},
	}
	for sel, want := range cases {
		got, err := ParsePages(sel, 5)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParsePages(%q) = %v, %v; want %v", sel, got, err, want)
		}
	}
	for _, bad := range []string{"0", "6", "3-2", "a", "1-x"} {
		if _, err := ParsePages(bad, 5); err == nil {
			t.Errorf("ParsePages(%q) should fail", bad)
		}
	}
}

func TestRotateRemoveExtract(t *testing.T) {
	ctx := context.Background()
	src := samplePDF(t, 4)
	if n := count(t, src, ""); n != 4 {
		t.Fatalf("pages = %d", n)
	}
	rotated, err := Rotate(ctx, src, []int{1, 3}, 90, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, rotated, ""); n != 4 {
		t.Fatalf("rotated pages = %d", n)
	}
	if _, err := Rotate(ctx, src, []int{1}, 45, ""); err == nil {
		t.Fatal("45 degrees should fail")
	}
	removed, err := Remove(ctx, src, []int{2, 3}, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, removed, ""); n != 2 {
		t.Fatalf("after remove = %d", n)
	}
	if _, err := Remove(ctx, src, []int{1, 2, 3, 4}, ""); !errors.Is(err, ErrAllPages) {
		t.Fatalf("removing all = %v", err)
	}
	extracted, err := Extract(ctx, src, []int{2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, extracted, ""); n != 1 {
		t.Fatalf("extracted = %d", n)
	}
}

func TestMergeSplit(t *testing.T) {
	ctx := context.Background()
	merged, err := Merge(ctx, [][]byte{samplePDF(t, 2), samplePDF(t, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, merged, ""); n != 5 {
		t.Fatalf("merged = %d", n)
	}
	parts, err := Split(ctx, merged, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || parts[0].From != 1 || parts[2].Thru != 5 {
		t.Fatalf("parts = %+v", parts)
	}
	for _, p := range parts {
		if n := count(t, p.Data, ""); n != p.Thru-p.From+1 {
			t.Fatalf("part %d-%d has %d pages", p.From, p.Thru, n)
		}
	}
	if _, err := Merge(ctx, [][]byte{merged}); err == nil {
		t.Fatal("merging one PDF should fail")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	ctx := context.Background()
	src := samplePDF(t, 2)
	if Encrypted(ctx, src) {
		t.Fatal("plain PDF reported encrypted")
	}
	locked, err := Encrypt(ctx, src, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !Encrypted(ctx, locked) {
		t.Fatal("encrypted PDF not reported")
	}
	if _, err := Decrypt(ctx, locked, "wrong"); err == nil {
		t.Fatal("wrong password should fail")
	}
	open, err := Decrypt(ctx, locked, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if Encrypted(ctx, open) || count(t, open, "") != 2 {
		t.Fatal("decrypted PDF is still locked or lost pages")
	}
	if _, err := Encrypt(ctx, src, ""); err == nil {
		t.Fatal("empty password should fail")
	}
}

func TestRejectsNonPDF(t *testing.T) {
	ctx := context.Background()
	if _, err := Rotate(ctx, []byte("hello"), []int{1}, 90, ""); err == nil {
		t.Fatal("non-PDF accepted")
	}
	if _, err := PageCount(ctx, []byte("%PDF-1.7 garbage"), ""); err == nil {
		t.Fatal("malformed PDF accepted")
	}
}
