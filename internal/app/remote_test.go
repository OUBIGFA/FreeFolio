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

	"github.com/OUBIGFA/FreeMD/internal/editor"
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
