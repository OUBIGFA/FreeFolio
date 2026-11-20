package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/fsutil"
	"github.com/egoist/mygo/ui"
)

const (
	// remoteImageLimit bounds a picture fetched from the web, so a link to
	// something huge cannot fill the memory or the cache.
	remoteImageLimit = 25 << 20
	remoteTimeout    = 30 * time.Second
)

// isRemoteImage reports whether an image's source is a web address.
func isRemoteImage(src string) bool {
	s := strings.ToLower(src)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// remoteImages shows pictures linked from the web: fetched in the
// background the first time a document shows one, then kept in memory for
// the session and on disk in the cache folder, so later launches show them
// without the network. The Markdown keeps its link. Its map is the main
// thread's; fetches report back through the post they were given.
type remoteImages struct {
	order   []string
	dir     string
	client  *http.Client
	entries map[string]*remoteImage
	// tweetEndpoint replaces X's embed service in tests (videoembed.go).
	tweetEndpoint string
}

type remoteImage struct {
	retired        atomic.Bool
	pixelsReleased bool
	resource       editor.ImageResource
	loading        bool
	// waiting are the editors to redraw once the fetch ends.
	waiting map[string]func()
}

func newRemoteImages(dir string) *remoteImages {
	return &remoteImages{dir: dir, client: &http.Client{Timeout: remoteTimeout}, entries: map[string]*remoteImage{}}
}

// cachePath is where the picture at url is kept on disk.
func (r *remoteImages) cachePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(r.dir, hex.EncodeToString(sum[:]))
}

// resource is the picture at url as shown now. With post, a picture not
// yet here is fetched on a goroutine and redraw runs once it arrives; it
// shows as loading meanwhile. Without post, as for thumbnails and PDF
// export, only what is already in memory or on disk shows.
func (r *remoteImages) resource(url string, post func(func()), redraw func(), owner ...string) editor.ImageResource {
	r.touch(url)
	key := "default"
	if len(owner) > 0 {
		key = owner[0]
	}
	if e := r.entries[url]; e != nil {
		if e.loading && redraw != nil {
			if e.waiting == nil {
				e.waiting = map[string]func(){}
			}
			e.waiting[key] = redraw
		}
		return e.resource
	}
	if isVideoFile(url) {
		// A video file plays from its address: nothing to fetch first.
		res := videoFileResource(url)
		r.entries[url] = &remoteImage{resource: res}
		return res
	}
	if post == nil {
		if isVideoEmbed(url) {
			if res, ok := r.cachedEmbed(url); ok {
				r.entries[url] = &remoteImage{resource: res}
				return res
			}
			return editor.ImageResource{Problem: editor.ImageEmbed}
		}
		if data, err := os.ReadFile(r.cachePath(url)); err == nil {
			res := decodeImageData(data, false)
			if res.Bitmap != nil {
				r.entries[url] = &remoteImage{resource: res}
				return res
			}
		}
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
	e := &remoteImage{resource: editor.ImageResource{Loading: true}, loading: true}
	if redraw != nil {
		if e.waiting == nil {
			e.waiting = map[string]func(){}
		}
		e.waiting[key] = redraw
	}
	r.entries[url] = e
	go func() {
		// Share the local-image decode bound. Evicted jobs never resurrect an
		// entry or retain editor callbacks after completing a stale request.
		if e.retired.Load() {
			return
		}
		imageDecodeSem <- struct{}{}
		if e.retired.Load() {
			<-imageDecodeSem
			return
		}
		res := r.fetch(url)
		<-imageDecodeSem
		if e.retired.Load() {
			return
		}
		post(func() {
			if e.retired.Load() {
				return
			}
			if res.Bitmap != nil {
				res.LoadedAt = time.Now()
			}
			e.resource, e.loading = res, false
			waiting := e.waiting
			e.waiting = nil
			for _, fn := range waiting {
				fn()
			}
		})
	}()
	return e.resource
}

// fetch reads the picture from the disk cache, or else from the web,
// keeping what it fetched. It runs off the main thread.
func (r *remoteImages) fetch(url string) editor.ImageResource {
	if isVideoEmbed(url) {
		return r.fetchEmbed(url)
	}
	path := r.cachePath(url)
	if data, err := os.ReadFile(path); err == nil {
		if res := decodeImageData(data, false); res.Bitmap != nil {
			return res
		}
	}
	data, err := r.download(url)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
	res := decodeImageData(data, false)
	if res.Bitmap == nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported}
	}
	// A cache that cannot be written only costs the next launch a fetch.
	if os.MkdirAll(r.dir, 0o755) == nil {
		_ = fsutil.WriteFileAtomic(path, data, 0o644)
	}
	return res
}

func (r *remoteImages) download(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FreeMD")
	req.Header.Set("Accept", "image/*")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote image: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, remoteImageLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > remoteImageLimit {
		return nil, errors.New("remote image: too large")
	}
	return data, nil
}

// raw is the cached picture at url for drawing into a PDF, or nil.
func (r *remoteImages) raw(url string) image.Image {
	data, err := os.ReadFile(r.cachePath(url))
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// decodeImageData decodes a picture's bytes, with its frames for a GIF;
// local tells whether it is a file the user can reveal.
func decodeImageData(data []byte, local bool) editor.ImageResource {
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported, Local: local}
	}
	w, h := bmp.Size()
	res := editor.ImageResource{Bitmap: bmp, Width: w, Height: h, Local: local}
	if frames, delays, ok := gifFrames(data); ok {
		res.Frames, res.Delays = frames, delays
	}
	return res
}

// touch bounds retained network bitmaps; live editors retain their own resources.
func (r *remoteImages) touch(url string) {
	for i, key := range r.order {
		if key == url {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.order = append(r.order, url)
	for len(r.order) > 64 {
		old := r.order[0]
		r.order = r.order[1:]
		if entry := r.entries[old]; entry != nil {
			entry.retired.Store(true)
		}
		delete(r.entries, old)
	}
}

// Decoded network images retain compressed disk/source data for restoration.
func (r *remoteImages) releasePixels(now time.Time) {
	for _, entry := range r.entries {
		res := entry.resource
		if entry.loading || entry.pixelsReleased || res.Bitmap == nil || res.LoadedAt.IsZero() || now.Sub(res.LoadedAt) < 200*time.Millisecond {
			continue
		}
		res.Bitmap.ReleasePixels()
		for _, frame := range res.Frames {
			if frame != nil {
				frame.ReleasePixels()
			}
		}
		entry.pixelsReleased = true
	}
}
