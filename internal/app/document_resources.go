package app

// isWhiteboard keeps the native scene check shared by document readers and
// local control, which must never treat a drawing as editable Markdown.
func (d *document) isWhiteboard() bool { return d != nil && d.board != nil }

// syncMovedPath reopens file-backed readers after their owning file moves.
func (d *document) syncMovedPath() {
	if d == nil || d.space == nil || d.rel == "" {
		return
	}
	if d.isPDF() {
		d.reopenPDF()
	} else if d.isMDZ() && d.mdz.archive != nil {
		if full, err := resolveFile(d.space, d.rel); err == nil {
			d.mdz.archive.SetPath(full)
		}
	}
}

// dismissPreview closes the transient preview before a document mutation.
func (a *App) dismissPreview() {
	a.closePreview(a.now)
	a.preview.shown = nil
	a.preview.closing = false
}

func (a *App) showMainWindow() {
	if a.win == nil {
		return
	}
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Show()
	a.win.Focus()
}
