package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// The card footer stays compact; this reader preserves the complete server
// response and supports selection, keyboard copying and a one-click copy.
func (b *Board) aiErrorDialog(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	if !s.errorOpen {
		return
	}
	ui.DialogBase(c.Key("ai-error-dialog"), &s.errorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(o.Theme.Background.Alpha(.65))
		w, h := c.Size()
		panel.Width(min(760, max(240, w-48))).MaxHeight(max(160, h-48)).Padding(16).Gap(12).Radius(10).Background(o.Theme.Surface).Border(1, o.Theme.Border).Label(i18n.T("完整错误"))
		ui.Text(c, s.errorTitle).FontSize(16).FontWeight(600)
		text := ui.TextArea(c.Key("ai-error-text"), &s.errorText).ReadOnly(true).WidthPercent(100).Height(min(460, max(80, h-180))).Label(i18n.T("完整错误内容"))
		if s.errorFocus {
			text.Focus().SetTextSelection(0, 0)
			s.errorFocus = false
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("copy-error"), i18n.T("复制完整错误")).Clicked() {
				b.services.WriteClipboard(s.errorText)
			}
			if ui.Button(c.Key("close-error"), i18n.T("关闭")).Clicked() {
				s.errorOpen = false
			}
		})
	})
}
