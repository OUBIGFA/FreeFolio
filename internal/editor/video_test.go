package editor

import (
	"errors"
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A picture that is a video's poster shows a play button, in a picture
// block and within a table's cell alike; pressing it asks the host for the
// video, and a video the system cannot play says so.
func TestVideoPostersPlay(t *testing.T) {
	poster := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 320, 180)))
	load := func(src string) ImageResource {
		return ImageResource{Bitmap: poster, Width: 320, Height: 180, Video: "https://video.example/" + src[len(src)-1:] + ".mp4"}
	}
	var opened []string
	open := func(url string) (*ui.VideoPlayer, error) {
		opened = append(opened, url)
		return nil, errors.New("no media framework in tests")
	}
	e := New("![](https://x.com/a/status/1/video/1)\n\n| ![](https://x.com/b/status/2) |\n| --- |\n| cell |\n")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), LoadImage: load, OpenVideo: open, OpenLink: func(string) {}})
	}, 900, 650)
	tt.Frame()
	tt.Frame()
	spots := e.videoSpots()
	if len(spots) != 2 {
		t.Fatalf("video spots %+v", spots)
	}
	if spots[0].url != "https://video.example/1.mp4" || spots[1].url != "https://video.example/2.mp4" {
		t.Fatalf("spots %+v", spots)
	}
	if err := tt.Click("播放视频"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(opened) != 1 || opened[0] != "https://video.example/1.mp4" {
		t.Fatalf("opened %v", opened)
	}
	if !tt.HasText("无法播放视频") || !tt.HasText("在浏览器中打开") {
		t.Fatal("a video that cannot play says so and offers its link")
	}
}
