package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

func TestRemoteImagesFetchCacheAndFail(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var pic bytes.Buffer
	png.Encode(&pic, img)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			hits++
			w.Write(pic.Bytes())
		case "/huge.png":
			w.Write(make([]byte, remoteImageLimit+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "images")
	posted := make(chan func(), 4)
	post := func(fn func()) { posted <- fn }
	r := newRemoteImages(dir)
	if !isRemoteImage(srv.URL+"/a.png") || isRemoteImage("assets/a.png") {
		t.Fatal("remote addresses")
	}
	redrawn := 0
	if res := r.resource(srv.URL+"/a.png", post, func() { redrawn++ }); !res.Loading {
		t.Fatal("a picture being fetched shows as loading", res)
	}
	(<-posted)()
	res := r.resource(srv.URL+"/a.png", post, nil)
	if res.Bitmap == nil || res.Width != 3 || res.Height != 2 || res.Local || redrawn != 1 {
		t.Fatalf("fetched: %#v, redrawn %d", res, redrawn)
	}
	if _, err := os.Stat(r.cachePath(srv.URL + "/a.png")); err != nil {
		t.Fatal("kept on disk", err)
	}
	// A later launch shows it from the disk, without the network, even
	// where only what is cached shows.
	later := newRemoteImages(dir)
	if res := later.resource(srv.URL+"/a.png", nil, nil); res.Bitmap == nil || hits != 1 {
		t.Fatal("from the cache", res, hits)
	}
	if res := later.resource(srv.URL+"/new.png", nil, nil); res.Problem != editor.ImageRemote || res.Loading {
		t.Fatal("not fetched without post", res)
	}
	for _, name := range []string{"/missing.png", "/huge.png"} {
		r.resource(srv.URL+name, post, nil)
		(<-posted)()
		if res := r.resource(srv.URL+name, post, nil); res.Bitmap != nil || res.Problem != editor.ImageRemote {
			t.Fatal(name, res)
		}
	}
}

// An SVG picture, as a shields.io badge, which the bitmap decoders reject,
// shows rasterized at twice its size and lays out at its own size; bytes
// that are neither stay unsupported.
func TestDecodeImageDataRasterizesSVG(t *testing.T) {
	const badge = `<svg xmlns="http://www.w3.org/2000/svg" width="60" height="20"><rect width="30" height="20" fill="#555"/><rect x="30" width="30" height="20" fill="#f21158"/><text x="15" y="14" fill="#fff" font-size="11" text-anchor="middle">MIT</text></svg>`
	res := decodeImageData([]byte(badge), false)
	if res.Bitmap == nil || res.Width != 60 || res.Height != 20 || res.Problem != 0 {
		t.Fatalf("svg: %+v", res)
	}
	if w, h := res.Bitmap.Size(); w != 120 || h != 40 {
		t.Fatalf("raster %dx%d", w, h)
	}
	if res := decodeImageData([]byte("not a picture"), true); res.Bitmap != nil || res.Problem != editor.ImageUnsupported || !res.Local {
		t.Fatalf("junk: %+v", res)
	}
}
