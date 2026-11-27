package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardEnhancementRunnerWritesNativeLoRAResult(t *testing.T) {
	a, tt := newTestApp(t)
	a.newWhiteboard("")
	tt.Frame()
	root := t.TempDir()
	a.prefs.ResourceDir = root
	// Keep actual catalog dispatch and verification, replacing only large bytes.
	old := localresource.ComfyPacks
	localresource.ComfyPacks = append([]localresource.ResourcePack(nil), old...)
	t.Cleanup(func() { localresource.ComfyPacks = old })
	weights := []byte("test adapter weights")
	for i, p := range localresource.ComfyPacks {
		if p.ID == "qwen-restore" {
			localresource.ComfyPacks[i].Files = append([]localresource.PackFile(nil), p.Files...)
			localresource.ComfyPacks[i].Files[0].Size = int64(len(weights))
			localresource.ComfyPacks[i].Files[0].SHA = fmt.Sprintf("%x", sha256.Sum256(weights))
			file := filepath.Join(root, p.Files[0].RelPath)
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, weights, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var picture bytes.Buffer
	_ = png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 32, 32)))
	var graph map[string]struct {
		Class  string         `json:"class_type"`
		Inputs map[string]any `json:"inputs"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			fmt.Fprint(w, `{"devices":[]}`)
		case "/object_info":
			info := map[string]any{}
			for _, name := range []string{"UNETLoader", "CLIPLoader", "VAELoader", "TextEncodeQwenImage21", "QwenImage21Cache", "ManualSigmas", "KSamplerSelect", "RandomNoise", "CFGGuider", "SamplerCustomAdvanced", "EmptyLatentImage", "VAEDecode"} {
				info[name] = map[string]any{}
			}
			for _, spec := range []struct{ node, key, file string }{{"UNETLoader", "unet_name", "qwen_image_2.1_turbo_int8_convrot.safetensors"}, {"CLIPLoader", "clip_name", "qwen3vl_8b_int8_convrot.safetensors"}, {"VAELoader", "vae_name", "qwen_image_2.1_vae_bf16.safetensors"}, {"LoraLoaderModelOnly", "lora_name", "freefolio-qwen-restore.safetensors"}} {
				info[spec.node] = map[string]any{"input": map[string]any{"required": map[string]any{spec.key: []any{[]string{spec.file}}}}}
			}
			_ = json.NewEncoder(w).Encode(info)
		case "/upload/image":
			fmt.Fprint(w, `{"name":"input.png"}`)
		case "/prompt":
			var payload struct {
				Prompt json.RawMessage `json:"prompt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_ = json.Unmarshal(payload.Prompt, &graph)
			fmt.Fprint(w, `{"prompt_id":"enhance"}`)
		case "/history/enhance":
			fmt.Fprint(w, `{"enhance":{"status":{"status_str":"success","completed":true},"outputs":{"13":{"images":[{"filename":"result.png","type":"output"}]}}}}`)
		case "/view":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(picture.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a.prefs.ComfyUI.URL = srv.URL
	runner, err := a.boardAIRunner(a.doc)
	if err != nil {
		t.Fatal(err)
	}
	strength := .5
	job := editor.BoardAIJob{Node: whiteboard.AINode{Role: whiteboard.AIEnhance, Connection: "comfyui-qwen-image", Model: "qwen-restore", Params: whiteboard.AIParams{LoRAStrength: &strength}}, Images: []editor.BoardAIImage{{Data: picture.Bytes(), MIME: "image/png"}}}
	result, err := runner.Media(context.Background(), job, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Output.Path == "" || !bytes.Equal(result[0].Data, picture.Bytes()) {
		t.Fatalf("enhancement attachment missing: %+v", result)
	}
	if graph["lora"].Inputs["strength_model"] != .5 || graph["lora"].Inputs["lora_name"] != "freefolio-qwen-restore.safetensors" {
		t.Fatal("runner lost adapter selection or strength")
	}
	prompt, _ := graph["5"].Inputs["prompt"].(string)
	if !strings.HasPrefix(prompt, "Restore the photo") {
		t.Fatalf("missing default restoration prompt: %q", prompt)
	}
	job.Node.Model = "qwen-image-2.1-turbo-int8"
	job.ModelFlow = &whiteboard.AIModelConfig{Connection: "comfyui-qwen-image", Model: job.Node.Model, LoRAs: []whiteboard.AILoRAConfig{{ID: "qwen-restore", Strength: &strength}}}
	result, err = runner.Media(context.Background(), job, nil)
	if err != nil || len(result) != 1 || graph["lora"].Inputs["strength_model"] != .5 {
		t.Fatalf("connected model chain failed: %v", err)
	}
}

func TestResourceTabsKeepModelsEnhancementsAndLoRASeparate(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabResources)
	tt.Frame()
	pathControl, ok := tt.Access("更改位置")
	if !ok {
		t.Fatal("shared resource directory is missing")
	}
	for _, node := range tt.AccessAll() {
		if node.Role == ui.RoleTab && node.Name == "本地模型" && node.Bounds.Y <= pathControl.Bounds.Y+pathControl.Bounds.H {
			t.Fatal("shared overview must be above resource tabs")
		}
	}
	for _, tc := range []struct {
		name, visible, hidden string
		index                 int
	}{
		{"ComfyUI 增强", "SeedVR2 3B (INT8)", "BEN2", 2},
		{"ComfyUI LoRA", "Z-Image Detailer", "SeedVR2 3B (INT8)", 3},
		{"ComfyUI 模型", "Z-Image-Turbo (INT8)", "BEN2", 1},
		{"本地模型", "BEN2", "Z-Image Detailer", 0},
	} {
		found := false
		for _, node := range tt.AccessAll() {
			if node.Role == ui.RoleTab && node.Name == tc.name {
				tt.ClickAt(node.Bounds.X+node.Bounds.W/2, node.Bounds.Y+node.Bounds.H/2)
				found = true
				break
			}
		}
		tt.Frame()
		if !found || a.resources.tab != tc.index || !tt.HasText(tc.visible) || tt.HasText(tc.hidden) {
			t.Fatalf("wrong tab content for %s", tc.name)
		}
		if a.resources.busy != "" {
			t.Fatal("switching tabs started a download")
		}
	}
}

func TestEnhancementResourcesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect resource tabs")
	a, tt := newTestApp(t)
	tt.SetSize(1240, 900)
	a.resources.gpuChecked, a.resources.gpuDone = true, true
	a.openSettings(tabResources)
	for _, tab := range []int{0, 1, 2, 3} {
		a.resources.tab = tab
		for range 3 {
			tt.Frame()
		}
		name := []string{"resources-local.png", "resources-comfy.png", "enhancement-models.png", "enhancement-loras.png"}[tab]
		if tab == 3 {
			name = "enhancement-loras.png"
		}
		savePNG(t, filepath.Join(dir, name), tt)
	}
	job := a.resourceTransferFor("z-detail")
	job.busy = "z-detail"
	job.total = 170127776
	job.received.Store(54000000)
	job.resume = func() {}
	job.cancel = func() {}
	tt.Frame()
	savePNG(t, filepath.Join(dir, "enhancement-downloading.png"), tt)
	job.cancel = nil
	job.paused = true
	tt.Frame()
	savePNG(t, filepath.Join(dir, "enhancement-paused.png"), tt)
	pack, _ := localresource.ComfyPack("z-detail")
	download, err := localresource.PackDownload(pack)
	if err != nil {
		t.Fatal(err)
	}
	a.setPackDownloadProgress(job, pack, download)
	job.received.Store(job.ownStart / 2)
	tt.Frame()
	savePNG(t, filepath.Join(dir, "lora-base-size.png"), tt)
}

func TestEnhancementNodesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect enhancement nodes")
	a, tt := newTestApp(t)
	tt.SetSize(1440, 1000)
	a.newWhiteboard("")
	tt.Frame()
	b := a.doc.board
	o := a.boardOptionsWith(nil, a.doc)
	for i, tc := range []struct {
		role      whiteboard.AIRole
		id, title string
		strength  float64
	}{
		{whiteboard.AIImage, "z-detail", "细节增强 · Z-Image", .35},
		{whiteboard.AIEnhance, "qwen-restore", "图片修复 · Qwen", 1},
	} {
		id := b.AddAINode(tc.role, whiteboard.Point{float64(250 + i*430), 350}, o)
		e := b.Scene.ElementByID(id)
		n := whiteboard.AINodeOf(e)
		n.Model, n.Title = tc.id, tc.title
		for _, preset := range a.boardEnhancementPresets() {
			if preset.Model.Model == tc.id {
				n.Connection = preset.Model.Connection
				n.Instruction = preset.Model.Instruction
			}
		}
		n.Params.LoRAStrength = &tc.strength
		if err := whiteboard.SetAINode(e, n); err != nil {
			t.Fatal(err)
		}
	}
	b.Selected = map[string]bool{}
	for range 3 {
		tt.Frame()
	}
	savePNG(t, filepath.Join(dir, "enhancement-nodes.png"), tt)
}

func TestModelFlowSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect model flow")
	a, tt := newTestApp(t)
	tt.SetSize(1600, 1000)
	a.newWhiteboard("")
	a.prefs.ComfyUI.Dir = t.TempDir()
	a.resources.dir = a.resourceDirectory()
	a.resources.installed = map[string]bool{"z-detail": true, "z-image-turbo-int8": true}
	a.resources.comfyCaps = &localresource.ComfyCapabilities{ZImageNative: true, LoRANative: true}
	tt.Frame()
	if err := tt.Click("AI 节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	savePNG(t, filepath.Join(dir, "model-sidebar.png"), tt)
	tt.Scroll(200, 700, 0, 650)
	tt.Frame()
	savePNG(t, filepath.Join(dir, "model-sidebar-loras.png"), tt)
	tt.Scroll(200, 400, 0, -2000)
	tt.Frame()
	search, ok := tt.Access("搜索节点")
	if !ok {
		t.Fatal("search missing")
	}
	tt.ClickAt(search.Bounds.X+20, search.Bounds.Y+10)
	tt.Type("Z-Image Detailer")
	tt.Frame()
	if err := tt.Click("细节增强 · Z-Image Detailer"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tt.Frame()
	}
	savePNG(t, filepath.Join(dir, "model-flow.png"), tt)
	for _, node := range tt.AccessAll() {
		if node.Role == ui.RolePopUpButton && node.Name == "模型" {
			tt.ClickAt(node.Bounds.X+20, node.Bounds.Y+10)
			break
		}
	}
	tt.Frame()
	savePNG(t, filepath.Join(dir, "model-selector.png"), tt)
}

func TestAllEnhancementPacksHaveMatchingNodes(t *testing.T) {
	a, _ := newTestApp(t)
	if model := a.boardAIDefault(whiteboard.AIImage); model.Model != "" {
		t.Fatal("an unavailable LoRA became the default image model")
	}
	presets := a.boardEnhancementPresets()
	for _, pack := range localresource.ComfyPacks {
		if pack.ResourceTab() == 1 {
			continue
		}
		found := false
		for _, preset := range presets {
			if preset.Model.Model != pack.ModelID {
				continue
			}
			found = true
			if pack.ID == "qwen-restore" && preset.Role != whiteboard.AIEnhance {
				t.Fatal("Qwen restore has no image input")
			}
			if pack.BaseModel != "" && preset.Model.LoRAStrength == nil {
				t.Fatal("LoRA has no strength")
			}
			if !preset.Model.NeedsDownload {
				t.Fatal("missing resources shown ready")
			}
		}
		if !found {
			t.Fatalf("no node for %s", pack.ID)
		}
	}
}
