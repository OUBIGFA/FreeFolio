package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
)

func TestWhiteboardSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to inspect the native whiteboard")
	for _, dark := range []bool{false, true} {
		name := "whiteboard"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.Frame()
			a.newWhiteboard("")
			tt.Frame()
			s, err := editor.MermaidBoard("flowchart LR\n A[客户端 Client] -->|请求| B{API 网关}\n B --> C[(数据库)]", whiteboard.Point{500, 300})
			if err != nil {
				t.Fatal(err)
			}
			a.doc.board = editor.NewBoard(s)
			tt.SetDark(dark)
			a.boardChanged(a.doc)
			tt.Frame()
			savePNG(t, filepath.Join(dir, name+".png"), tt)
		})
	}
}
