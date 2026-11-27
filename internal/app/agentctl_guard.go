package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/OUBIGFA/FreeFolio/internal/space"
)

// checkControlWrite applies the same read-only and external-change contract as
// a local edit before the agent changes any live model or file.
func (a *App) checkControlWrite(sp *space.Space, rel string) error {
	d := a.residentDocument(sp.ID, rel)
	if d == nil {
		return nil
	}
	d.waitAsyncSave()
	if d.loadErr != nil {
		return d.loadErr
	}
	if d.ed == nil || d.ed.ReadOnly || d.ed.Locked || d.converted || d.isPDF() || d.isMDZ() || d.board != nil && d.board.ReadOnly {
		return errors.New("document is read-only")
	}
	if d.sourceComposing || d.ed.Composing() {
		return errors.New("finish text composition before editing through a local agent")
	}
	disk, err := readFile(sp, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errMissing
		}
		return err
	}
	if d.conflict || d.missing || disk != d.diskText {
		return fmt.Errorf("%s: %w", rel, errConflict)
	}
	return nil
}
