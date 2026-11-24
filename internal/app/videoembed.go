package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

// Videos embedded as images, as Obsidian shows them: ![](address) of a
// video file plays where it stands, and so does the video of a post on X
// (x.com or twitter.com). A post's video and its poster come from X's
// embed service, the one its embedded posts read; a post with pictures and
// no video shows its picture. The Markdown keeps its link.

// tweetRe matches the address of a post on X, with the media it names, as
// …/status/<id>/video/2 does.
var tweetRe = regexp.MustCompile(`^https?://(?:www\.|mobile\.)?(?:x|twitter)\.com/(?:i/web|i|[A-Za-z0-9_]+)/status(?:es)?/(\d+)(?:/(?:video|photo)/(\d+))?/?(?:[?#].*)?$`)

// tweetEmbed returns the post an address names and the 1-based number of
// its media it names, 0 for the first video or else picture.
func tweetEmbed(url string) (id string, nth int, ok bool) {
	m := tweetRe.FindStringSubmatch(url)
	if m == nil {
		return "", 0, false
	}
	if m[2] != "" {
		nth, _ = strconv.Atoi(m[2])
	}
	return m[1], nth, true
}

// isVideoFile reports whether an address names a video file or stream by
// its extension.
func isVideoFile(url string) bool {
	u, err := neturl.Parse(url)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".mp4", ".m4v", ".mov", ".webm", ".m3u8":
		return true
	}
	return false
}

// isVideoEmbed reports whether an image's address shows as a video, or as
// a post's picture, rather than as a picture fetched from it.
func isVideoEmbed(url string) bool {
	_, _, tweet := tweetEmbed(url)
	return tweet || isVideoFile(url)
}

// openVideo opens a video to play inline: a web address as a stream, else
// a file of the space, resolved by imageResourceFor.
func openVideo(src string) (*ui.VideoPlayer, error) {
	if isRemoteImage(src) {
		return video.OpenURL(src)
	}
	return video.Open(src)
}

// videoPoster is what a video file shows before it plays: a dark 16:9
// frame, under the play button the editor draws.
var videoPoster = func() *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	fill := color.RGBA{0x1f, 0x21, 0x24, 0xff}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = fill.R, fill.G, fill.B, fill.A
	}
	return ui.NewBitmap(img)
}()

// videoFileResource is a video file's resource: the poster, playing the
// file's address.
func videoFileResource(url string) editor.ImageResource {
	w, h := videoPoster.Size()
	return editor.ImageResource{Bitmap: videoPoster, Width: w, Height: h, Video: url}
}

// tweetMedia is a medium of a post, as X's embed service tells it.
type tweetMedia struct {
	Type      string `json:"type"`
	Poster    string `json:"media_url_https"`
	VideoInfo struct {
		Variants []struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		} `json:"variants"`
	} `json:"video_info"`
}

// pick returns the picture to show for the media of a post and, for a
// video, the address of its MP4 at the highest bitrate: the nth video or
// picture, or with nth 0 the first video, else the first medium.
func pickTweetMedia(media []tweetMedia, nth int) (poster, video string, err error) {
	var chosen *tweetMedia
	switch {
	case nth > 0 && nth <= len(media):
		chosen = &media[nth-1]
	case nth == 0:
		for i := range media {
			if media[i].Type != "photo" {
				chosen = &media[i]
				break
			}
		}
		if chosen == nil && len(media) > 0 {
			chosen = &media[0]
		}
	}
	if chosen == nil {
		return "", "", errors.New("post: no such media")
	}
	best := -1
	for _, v := range chosen.VideoInfo.Variants {
		if v.ContentType == "video/mp4" && strings.HasPrefix(v.URL, "https://") && v.Bitrate > best {
			best, video = v.Bitrate, v.URL
		}
	}
	if chosen.Type != "photo" && video == "" {
		return "", "", errors.New("post: no playable video")
	}
	if !strings.HasPrefix(chosen.Poster, "https://") {
		return "", "", errors.New("post: no picture")
	}
	return chosen.Poster, video, nil
}

// tweetToken is the token X's embed service asks of a post's id, as its
// embedded posts compute it: (id / 1e15 · π) in base 36, without its
// zeros and point.
func tweetToken(id string) string {
	n, err := strconv.ParseFloat(id, 64)
	if err != nil {
		return "a"
	}
	v := n / 1e15 * math.Pi
	whole := math.Floor(v)
	s := strconv.FormatInt(int64(whole), 36) + "."
	frac := v - whole
	for i := 0; i < 11 && frac > 0; i++ {
		frac *= 36
		d := int(frac)
		s += strconv.FormatInt(int64(d), 36)
		frac -= float64(d)
	}
	s = strings.NewReplacer("0", "", ".", "").Replace(s)
	if s == "" {
		return "a"
	}
	return s
}

// videoSidecar is where the address a cached poster plays is kept.
func (r *remoteImages) videoSidecar(url string) string { return r.cachePath(url) + ".video" }

// cachedEmbed is the cached resource of an embedded post or video, for
// the session or a later launch without the network.
func (r *remoteImages) cachedEmbed(url string) (editor.ImageResource, bool) {
	if isVideoFile(url) {
		return videoFileResource(url), true
	}
	data, err := os.ReadFile(r.cachePath(url))
	if err != nil {
		return editor.ImageResource{}, false
	}
	res := decodeImageData(data, false)
	if res.Bitmap == nil {
		return editor.ImageResource{}, false
	}
	if video, err := os.ReadFile(r.videoSidecar(url)); err == nil {
		res.Video = strings.TrimSpace(string(video))
	}
	return res, true
}

// fetchEmbed resolves an embedded post on X to its poster and video and
// keeps both, off the main thread. A post that is gone (X answers with a
// tombstone and no media) or cannot be reached is ImageEmbed: the editor
// shows a notice, not a picture to choose again.
func (r *remoteImages) fetchEmbed(url string) editor.ImageResource {
	if res, ok := r.cachedEmbed(url); ok {
		return res
	}
	id, nth, _ := tweetEmbed(url)
	media, err := r.tweetMedia(id)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageEmbed}
	}
	poster, video, err := pickTweetMedia(media, nth)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageEmbed}
	}
	data, err := r.download(poster)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageEmbed}
	}
	res := decodeImageData(data, false)
	if res.Bitmap == nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported}
	}
	res.Video = video
	// A cache that cannot be written only costs the next launch a fetch.
	if os.MkdirAll(r.dir, 0o755) == nil {
		if video != "" {
			_ = fsutil.WriteFileAtomic(r.videoSidecar(url), []byte(video), 0o644)
		}
		_ = fsutil.WriteFileAtomic(r.cachePath(url), data, 0o644)
	}
	return res
}

// tweetMedia reads the media of post id from X's embed service.
func (r *remoteImages) tweetMedia(id string) ([]tweetMedia, error) {
	endpoint := r.tweetEndpoint
	if endpoint == "" {
		endpoint = "https://cdn.syndication.twimg.com/tweet-result"
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?id="+id+"&lang=en&token="+tweetToken(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FreeFolio")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("post: %s", resp.Status)
	}
	var post struct {
		MediaDetails []tweetMedia `json:"mediaDetails"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&post); err != nil {
		return nil, err
	}
	return post.MediaDetails, nil
}
