package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestLocalNodeRequiresDownloadDependenciesAndEngineSupport(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.ComfyUI.Dir = t.TempDir()
	a.resources.installed = map[string]bool{}
	m := editor.BoardAIModel{Connection: "comfyui-zimage", Model: "z-detail"}
	if got := a.boardAI.boardModelAvailability(m); !got.NeedsDownload || got.Unavailable == "" {
		t.Fatal("missing LoRA enabled")
	}
	a.resources.installed["z-detail"] = true
	if !a.boardAI.boardModelAvailability(m).NeedsDownload {
		t.Fatal("missing base enabled")
	}
	a.resources.installed["z-image-turbo-int8"] = true
	if got := a.boardAI.boardModelAvailability(m); got.NeedsDownload || got.Unavailable != "" {
		t.Fatalf("downloaded model cannot start the engine on demand: %+v", got)
	}
	a.resources.comfyCaps = &localresource.ComfyCapabilities{ZImageNative: true, LoRANative: true}
	if got := a.boardAI.boardModelAvailability(m); got.NeedsDownload || got.Unavailable != "" {
		t.Fatalf("ready model disabled: %+v", got)
	}
	a.resources.comfyCaps.LoRANative = false
	if a.boardAI.boardModelAvailability(m).Unavailable == "" {
		t.Fatal("missing native LoRA node enabled")
	}
}

func TestBoardComfyStaysIdleUntilNodeRun(t *testing.T) {
	for _, state := range []string{"not-started", "stopped", "failed-connection"} {
		t.Run(state, func(t *testing.T) {
			a, tt := newTestApp(t)
			// An isolated missing engine makes a startup attempt observable without
			// launching Python or touching the user's ComfyUI installation.
			a.prefs.ComfyUI.Dir = filepath.Join(t.TempDir(), "missing-engine")
			a.resources.dir = a.resources.resourceDirectory()
			a.resources.installed = map[string]bool{"z-image-turbo-int8": true}
			switch state {
			case "stopped":
				a.resources.comfyCaps = &localresource.ComfyCapabilities{ZImageNative: true}
				a.resources.stopComfyUI()
				if a.resources.comfyCaps != nil {
					t.Fatal("stopping retained stale engine capabilities")
				}
			case "failed-connection":
				a.resources.comfyErr = errors.New("previous connection failed")
			}
			previousErr := a.resources.comfyErr
			a.newWhiteboard("")
			tt.Frame()
			if err := tt.Click("AI 节点"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			model := a.boardAI.boardAIDefault(whiteboard.AIImage)
			if model.Connection != "comfyui-zimage" || model.Model != "z-image-turbo-int8" {
				t.Fatalf("idle engine blocked model selection: %+v", model)
			}
			if a.resources.comfyBusy || a.resources.comfyCaps != nil || a.resources.comfyErr != previousErr || a.resources.comfy != nil && a.resources.comfy.Running() {
				t.Fatal("browsing the sidebar changed the engine connection")
			}
			o := a.boardOptionsWith(nil, a.doc)
			id := a.doc.board.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
			el := a.doc.board.Scene.ElementByID(id)
			n := whiteboard.AINodeOf(el)
			n.Instruction = "a cat"
			if err := whiteboard.SetAINode(el, n); err != nil {
				t.Fatal(err)
			}
			a.doc.board.RunAI([]string{id}, false, true, o)
			if err := whiteboard.AINodeOf(el).Error; !strings.Contains(err, "directory does not exist") {
				t.Fatalf("running the node did not attempt the configured engine: %s", err)
			}
		})
	}
}

func TestSidebarUnavailablePresetIsDisabledAndCannotInsert(t *testing.T) {
	a, tt := newTestApp(t)
	a.newWhiteboard("")
	tt.Frame()
	b := a.doc.board
	o := a.boardOptionsWith(nil, a.doc)
	if err := tt.Click("AI 节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	search, ok := tt.Access("搜索节点")
	if !ok {
		t.Fatal("search missing")
	}
	tt.ClickAt(search.Bounds.X+12, search.Bounds.Y+10)
	tt.Type("Z-Image Detailer")
	tt.Frame()
	found := false
	for _, n := range tt.AccessAll() {
		if n.Role == ui.RoleButton && strings.Contains(n.Name, "Z-Image Detailer") {
			found = true
			if !n.Disabled {
				t.Fatal("missing download is clickable")
			}
		}
	}
	if !found {
		t.Fatal("unavailable preset hidden")
	}
	before := len(b.Scene.Elements)
	for _, preset := range o.AI.Presets() {
		if preset.Model.Model == "z-detail" {
			if !preset.Model.NeedsDownload || preset.Model.Unavailable == "" {
				t.Fatal("missing preset marked ready")
			}
		}
	}
	if len(b.Scene.Elements) != before {
		t.Fatal("listing unavailable preset inserts nodes")
	}
}

func TestLoRADescriptionDistinguishesRepairTasks(t *testing.T) {
	a, _ := newTestApp(t)
	for _, id := range []string{"qwen-restore", "qwen-mosaic"} {
		p, _ := localresource.ComfyPack(id)
		_, _, detail := a.resources.packDetailAndBadge(p)
		if !strings.Contains(detail, p.Description) || p.Description == "" {
			t.Fatal("resource row missing purpose")
		}
	}
	if a.boardAI.boardRoleUnavailable(whiteboard.AIAudio) == "" {
		t.Fatal("audio available without downloads")
	}
}

func TestDownloadedLoRAEnablesOnlyWhenItsFilesAreMounted(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.ComfyUI.Dir = t.TempDir()
	a.resources.dir = a.resources.resourceDirectory()
	a.resources.installed = map[string]bool{"z-detail": true, "z-image-turbo-int8": true}
	a.resources.comfyCaps = &localresource.ComfyCapabilities{ZImageNative: true, LoRANative: true, ModelFiles: map[string][]string{}}
	model := editor.BoardAIModel{Connection: "comfyui-zimage", Model: "z-detail"}
	if a.boardAI.boardModelAvailability(model).Unavailable == "" {
		t.Fatal("unmounted weights enabled")
	}
	pack, _ := localresource.ComfyPack("z-detail")
	plan, _ := localresource.PackDownload(pack)
	for _, f := range plan.Files {
		parts := strings.Split(f.RelPath, "/")
		a.resources.comfyCaps.ModelFiles[parts[1]] = append(a.resources.comfyCaps.ModelFiles[parts[1]], parts[len(parts)-1])
	}
	if got := a.boardAI.boardModelAvailability(model); got.Unavailable != "" || got.NeedsDownload {
		t.Fatalf("verified available model disabled: %+v", got)
	}
	if a.boardAI.boardRoleUnavailable(whiteboard.AILoRA) != "" {
		t.Fatal("ready LoRA role disabled")
	}
}
