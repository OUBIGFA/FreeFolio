package editor

import (
	"github.com/OUBIGFA/FreeMD/internal/solar"
	"strconv"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// Videos shown as images (ImageResource.Video): the poster shows where the
// picture would, with a play button over it; pressed, the video plays in
// the poster's place, inline, as Obsidian's embeds do. One plays at a
// time: starting another closes the last. A click on a playing video
// pauses or resumes it.

var (
	iconPlay = solar.Must("play@bold")
)

// videoState is the video playing, or paused, in the page: key names its
// place (a picture block's ID, or a row's and an inline object's).
type videoState struct {
	key, src string
	player   *ui.VideoPlayer
	err      error
}

// videoSpot is a video's place in view, in the surface's coordinates.
type videoSpot struct {
	key, src, url string
	rect          ui.Rect
}

// videoSpots are the videos in view: picture blocks, and images within
// text, as a table's cells hold them.
func (e *Editor) videoSpots() []videoSpot {
	var spots []videoSpot
	for i := range e.visibleRows(0) {
		r := &e.rows[i]
		if r.rect.Y+r.rect.H < e.Scroll.Y || r.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		if f := r.picture; f != nil && f.resource.Video != "" && f.resource.Bitmap != nil {
			spots = append(spots, videoSpot{key: r.leaf.Block.ID, src: r.leaf.Block.URL, url: f.resource.Video, rect: f.bounds})
		}
		if r.layout == nil {
			continue
		}
		dx, dy := e.visualOffset(*r)
		for j, o := range r.layout.objects {
			if o.video == "" || o.picture == nil {
				continue
			}
			rect := o.rect
			rect.X += r.x + dx
			rect.Y += r.y + dy
			spots = append(spots, videoSpot{key: r.leaf.Block.ID + ":" + strconv.Itoa(r.leaf.Start) + ":" + strconv.Itoa(j), src: o.source, url: o.video, rect: rect})
		}
	}
	return spots
}

// playVideo starts the video at spot, closing the one before.
func (e *Editor) playVideo(s videoSpot) {
	e.stopVideo()
	e.video = videoState{key: s.key, src: s.src}
	if e.openVideo == nil {
		return
	}
	e.video.player, e.video.err = e.openVideo(s.url)
}

// stopVideo closes the video playing, if any.
func (e *Editor) stopVideo() {
	if e.video.player != nil {
		e.video.player.Close()
	}
	e.video = videoState{}
}

// videoControls are the play buttons over the posters in view and the
// video playing, in the surface's coordinates.
func (e *Editor) videoControls(c *ui.Context) {
	if e.printing {
		return
	}
	pg := e.page
	for _, s := range e.videoSpots() {
		r := s.rect
		key := func(part string) *ui.Context { return c.Key("video-" + part + ":" + s.key) }
		if e.video.key == s.key && e.video.player != nil && e.video.err == nil && e.video.player.Err() == nil {
			p := e.video.player
			v := ui.Video(key("player"), p).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Fit(ui.Contain).Radius(4).Clip().
				Background(ui.RGB(0, 0, 0)).Focusable().Role(ui.RoleButton).Cursor(ui.CursorPointer).
				Label(i18n.T("视频")).Tooltip(s.src)
			toggle := func() {
				if p.Playing() {
					p.Pause()
				} else {
					if p.Ended() {
						p.Seek(0)
					}
					p.Play()
				}
			}
			if v.Clicked() || v.Focused() && (v.Shortcut(0, ui.KeySpace) || v.Shortcut(0, ui.KeyEnter)) {
				toggle()
			}
			if v.Focused() && v.Shortcut(0, ui.KeyEscape) {
				e.stopVideo()
				continue
			}
			v.ContextMenu(func(m *ui.Menu) {
				label := i18n.T("暂停")
				if !p.Playing() {
					label = i18n.T("播放")
				}
				menuAction(m, label, toggle)
				menuAction(m, i18n.T("停止播放"), e.stopVideo)
				if e.openLink != nil {
					menuAction(m, i18n.T("在浏览器中打开"), func() { e.openLink(s.src) })
				}
			})
			if !p.Playing() {
				e.videoBadge(c, key("paused"), r, iconPlay).IgnoreInput(true)
			}
			continue
		}
		if e.video.key == s.key && (e.video.err != nil || e.video.player != nil && e.video.player.Err() != nil || e.openVideo == nil) {
			// The system cannot play it: say so, and offer the link.
			ui.Column(key("failed")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Center().Gap(6).
				Background(pg.Scrim).Radius(4).Children(func() {
				ui.Text(c, i18n.T("无法播放视频")).FontSize(13).TextColor(pg.OnScrim)
				if e.openLink != nil && r.H >= 64 {
					if ui.Button(key("open"), i18n.T("在浏览器中打开")).Clicked() {
						e.openLink(s.src)
						e.stopVideo()
					}
				}
			})
			continue
		}
		if b := e.videoBadge(c, key("play"), r, iconPlay); b.Clicked() {
			e.playVideo(s)
		}
	}
	// The video playing went out of the page, as its block was deleted.
	if e.video.key != "" && !e.videoShown() {
		e.stopVideo()
	}
}

// videoShown reports whether the video playing is still in the page,
// in view or not.
func (e *Editor) videoShown() bool {
	if e.video.key == "" {
		return false
	}
	for i := range e.rows {
		r := &e.rows[i]
		if r.picture != nil && r.leaf.Block.ID == e.video.key && r.leaf.Block.URL == e.video.src {
			return true
		}
		if r.layout != nil {
			for j, o := range r.layout.objects {
				if o.source == e.video.src && r.leaf.Block.ID+":"+strconv.Itoa(r.leaf.Start)+":"+strconv.Itoa(j) == e.video.key {
					return true
				}
			}
		}
	}
	return false
}

// videoBadge is the round play button at the middle of r, smaller over a
// small poster.
func (e *Editor) videoBadge(c *ui.Context, key *ui.Context, r ui.Rect, icon *ui.SVG) ui.Element {
	size := min(float32(56), max(28, min(r.W, r.H)/3))
	b := ui.ButtonBase(key).Absolute().Left(r.X+(r.W-size)/2).Top(r.Y+(r.H-size)/2).Size(size, size).
		Radius(size/2).Center().Background(e.page.Scrim.Alpha(0.72)).Border(1.5, e.page.OnScrim.Alpha(0.55)).Label(i18n.T("播放视频")).Tooltip(i18n.T("播放视频")).
		Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(e.page.Scrim.Alpha(0.88))
	}
	if b.FocusVisible() {
		b.Border(2, c.Theme().Accent)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(size * 0.4).TextColor(e.page.OnScrim) })
	return b
}
