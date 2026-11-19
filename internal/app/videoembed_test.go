package app

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestVideoEmbedAddresses(t *testing.T) {
	for _, c := range []struct {
		url string
		id  string
		nth int
		ok  bool
	}{
		{"https://x.com/MoQiuyao201314/status/2065089148751577286/video/1", "2065089148751577286", 1, true},
		{"https://x.com/i/status/1930080468529230100", "1930080468529230100", 0, true},
		{"https://twitter.com/a_b/status/42?s=20", "42", 0, true},
		{"https://mobile.twitter.com/a/status/42/photo/2", "42", 2, true},
		{"https://x.com/a", "", 0, false},
		{"https://pbs.twimg.com/media/GwHiyUsWoAAzp9o?format=jpg&name=large", "", 0, false},
	} {
		id, nth, ok := tweetEmbed(c.url)
		if id != c.id || nth != c.nth || ok != c.ok {
			t.Errorf("%s: %q %d %v", c.url, id, nth, ok)
		}
	}
	if !isVideoFile("https://example.com/a/clip.MP4?x=1") || !isVideoFile("media/clip.webm") || isVideoFile("https://example.com/a.png") {
		t.Fatal("video files by extension")
	}
	if res := videoFileResource("clip.mp4"); res.Video != "clip.mp4" || res.Bitmap == nil {
		t.Fatalf("a video file shows its poster: %+v", res)
	}
}

func TestPickTweetMedia(t *testing.T) {
	photo := tweetMedia{Type: "photo", Poster: "https://p/1.jpg"}
	clip := tweetMedia{Type: "video", Poster: "https://p/2.jpg"}
	clip.VideoInfo.Variants = append(clip.VideoInfo.Variants,
		struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		}{0, "application/x-mpegURL", "https://v/pl.m3u8"},
		struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		}{256000, "video/mp4", "https://v/low.mp4"},
		struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		}{2176000, "video/mp4", "https://v/high.mp4"})
	media := []tweetMedia{photo, clip}
	if poster, video, err := pickTweetMedia(media, 0); err != nil || poster != "https://p/2.jpg" || video != "https://v/high.mp4" {
		t.Fatalf("first video at its best: %q %q %v", poster, video, err)
	}
	if poster, video, err := pickTweetMedia(media, 1); err != nil || poster != "https://p/1.jpg" || video != "" {
		t.Fatalf("the named picture: %q %q %v", poster, video, err)
	}
	if _, _, err := pickTweetMedia(media, 3); err == nil {
		t.Fatal("no third medium")
	}
	if tok := tweetToken("2065089148751577286"); tok == "" || bytes.ContainsAny([]byte(tok), "0.") {
		t.Fatalf("token %q", tok)
	}
}

// A post's video resolves through the embed service to its poster and MP4,
// kept on disk so a later launch shows it without the network.
func TestFetchTweetVideo(t *testing.T) {
	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tweet-result":
			if r.URL.Query().Get("id") != "2065089148751577286" || r.URL.Query().Get("token") == "" {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"mediaDetails":[{"type":"video","media_url_https":%q,"video_info":{"variants":[{"bitrate":832000,"content_type":"video/mp4","url":"https://video.example/a.mp4"}]}}]}`, srv.URL+"/thumb.png")
		case "/thumb.png":
			w.Write(pic.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "images")
	r := newRemoteImages(dir)
	r.client, r.tweetEndpoint = srv.Client(), srv.URL+"/tweet-result"
	url := "https://x.com/MoQiuyao201314/status/2065089148751577286/video/1"
	res := r.fetch(url)
	if res.Bitmap == nil || res.Width != 4 || res.Video != "https://video.example/a.mp4" {
		t.Fatalf("resolved %+v", res)
	}
	later := newRemoteImages(dir)
	if res := later.resource(url, nil, nil); res.Bitmap == nil || res.Video != "https://video.example/a.mp4" {
		t.Fatalf("from the cache %+v", res)
	}
	if res := r.fetch("https://x.com/a/status/1"); res.Bitmap != nil || res.Problem == 0 {
		t.Fatalf("a post that is gone %+v", res)
	}
}
