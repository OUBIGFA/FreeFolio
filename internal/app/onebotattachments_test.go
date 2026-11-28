package app

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/bmp"
)

func writeZipFile(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func bmpFile(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	img.Set(2, 2, color.RGBA{R: 220, G: 40, B: 40, A: 255})
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOneBotAttachmentsKeepTextAndRejectBinary(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	os.WriteFile(file, []byte("中文参考"), 0600)
	part, err := oneBotAttachment(file)
	if err != nil || part.Type != "text" || !strings.Contains(part.Text, "中文参考") {
		t.Fatalf("text %+v %v", part, err)
	}
	file = filepath.Join(dir, "binary.exe")
	os.WriteFile(file, []byte{0, 1, 2, 3}, 0600)
	if _, err = oneBotAttachment(file); err == nil {
		t.Fatal("binary accepted as text")
	}
	file = filepath.Join(dir, "page.pdf")
	os.WriteFile(file, []byte("%PDF-test"), 0600)
	part, err = oneBotAttachment(file)
	if err != nil || part.Type != "file" || !strings.HasPrefix(part.Data, "data:application/pdf;base64,") {
		t.Fatalf("PDF %+v %v", part, err)
	}
}

func TestOneBotAttachmentsSupportOfficeDocsBMPAndFolders(t *testing.T) {
	dir := t.TempDir()

	docxPath := filepath.Join(dir, "spec.docx")
	writeZipFile(t, docxPath, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>需求说明</w:t></w:r></w:p><w:p><w:r><w:t>第一条</w:t><w:tab/><w:t>完成拖拽</w:t></w:r></w:p></w:body></w:document>`,
	})
	item, err := buildOneBotAttachment(docxPath, nil)
	if err != nil || item.Kind != "office" || item.Part.Type != "text" || !strings.Contains(item.Part.Text, "需求说明\n第一条\t完成拖拽") {
		t.Fatalf("docx failed: %+v err=%v", item, err)
	}

	xlsxPath := filepath.Join(dir, "metrics.xlsx")
	writeZipFile(t, xlsxPath, map[string]string{
		"xl/sharedStrings.xml":     `<?xml version="1.0" encoding="UTF-8"?><sst><si><t>模块</t></si><si><t>耗时</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?><worksheet><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row><row><c t="inlineStr"><is><t>OneBot</t></is></c><c><v>120</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<?xml version="1.0" encoding="UTF-8"?><worksheet><sheetData><row><c t="inlineStr"><is><t>第二页</t></is></c></row></sheetData></worksheet>`,
	})
	item, err = buildOneBotAttachment(xlsxPath, nil)
	if err != nil || item.Kind != "office" || !strings.Contains(item.Part.Text, "模块\t耗时\nOneBot\t120") || !strings.Contains(item.Part.Text, "Sheet 2:\n第二页") {
		t.Fatalf("xlsx failed: %+v err=%v", item, err)
	}

	pptxPath := filepath.Join(dir, "deck.pptx")
	writeZipFile(t, pptxPath, map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>架构评审</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})
	item, err = buildOneBotAttachment(pptxPath, nil)
	if err != nil || item.Kind != "office" || !strings.Contains(item.Part.Text, "Slide 1:\n架构评审") {
		t.Fatalf("pptx failed: %+v err=%v", item, err)
	}

	bmpPath := filepath.Join(dir, "shot.bmp")
	bmpFile(t, bmpPath)
	item, err = buildOneBotAttachment(bmpPath, nil)
	if err != nil || item.Kind != "image" || item.Part.Type != "image" || item.Part.MIME != "image/png" || !strings.HasPrefix(item.Part.Data, "data:image/png;base64,") || item.Thumb == nil {
		t.Fatalf("bmp failed: %+v err=%v", item, err)
	}

	arcmdPath := filepath.Join(dir, "legacy.arcmd")
	encoded, err := editor.ImportMarkdown("# 标题\n\n正文段落").Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arcmdPath, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err = buildOneBotAttachment(arcmdPath, nil)
	if err != nil || item.Kind != "doc" || !strings.Contains(item.Part.Text, "# 标题") || strings.Contains(item.Part.Text, `"blocks"`) {
		t.Fatalf("arcmd failed: %+v err=%v", item, err)
	}

	folder := filepath.Join(dir, "bundle")
	os.MkdirAll(filepath.Join(folder, ".git"), 0o755)
	os.MkdirAll(filepath.Join(folder, "sub"), 0o755)
	os.WriteFile(filepath.Join(folder, ".git", "config"), []byte("hidden"), 0o600)
	os.WriteFile(filepath.Join(folder, "a.md"), []byte("A"), 0o600)
	os.WriteFile(filepath.Join(folder, "sub", "b.txt"), []byte("B"), 0o600)
	os.WriteFile(filepath.Join(folder, "sub", "c.txt"), []byte("C"), 0o600)
	expanded, truncated := expandDroppedPaths([]string{folder}, 2)
	if len(expanded) != 2 || !truncated {
		t.Fatalf("folder expand = %v truncated=%v", expanded, truncated)
	}
}

func TestOneBotDropMultiFilesImagesAndEditInConversation(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		func(req agent.Request) agent.Message {
			u := req.Messages[len(req.Messages)-1]
			if len(u.Parts) != 3 {
				t.Fatalf("expected 3 parts in request, got %+v", u.Parts)
			}
			hasImage, hasPlan, hasExt := false, false, false
			for _, p := range u.Parts {
				if p.Type == "image" && p.Name == "photo.png" {
					hasImage = true
				}
				if p.Type == "text" && strings.Contains(p.Text, "旧计划内容") && strings.Contains(p.Text, `space_path="Plan.md"`) {
					hasPlan = true
				}
				if p.Type == "text" && strings.Contains(p.Text, "外部参考") {
					hasExt = true
				}
			}
			if !hasImage || !hasPlan || !hasExt {
				t.Fatalf("missing attached parts: %+v", u.Parts)
			}
			return agent.Message{
				Role: agent.Assistant,
				Calls: []agent.ToolCall{{
					ID:   "e1",
					Name: "edit",
					Args: `{"path":"Plan.md","oldString":"旧计划内容","newString":"更新后的计划"}`,
				}},
			}
		},
		reply("已结合图片和参考资料更新 Plan.md。"),
	}}
	a, tt := qaApp(t, model)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "Plan.md", "# Plan\n\n旧计划内容"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Welcome.md")
	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	extDir := t.TempDir()
	extMD := filepath.Join(extDir, "ext.md")
	os.WriteFile(extMD, []byte("外部参考"), 0o600)
	pdfPath := filepath.Join(extDir, "spec.pdf")
	os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o600)
	picPath, _ := pngFile(t, "photo.png")
	planDisk := filepath.Join(sp.Root, "Plan.md")

	inputBox, ok := tt.Find("问题")
	if !ok {
		t.Fatal("OneBot input not found")
	}
	if !tt.DropFiles(inputBox.X+10, inputBox.Y+10, picPath, extMD, planDisk, pdfPath) {
		t.Fatal("OneBot did not accept dropped files")
	}
	tt.Frame()

	if len(a.qa.attachments) != 4 {
		t.Fatalf("attachments = %+v", a.qa.attachments)
	}
	if a.qa.attachments[0].Thumb == nil {
		t.Fatal("expected decoded thumbnail for dropped PNG")
	}
	if a.qa.attachments[2].SpaceID != sp.ID || a.qa.attachments[2].RelPath != "Plan.md" || !a.qa.attachments[2].Editable {
		t.Fatalf("space file metadata lost: %+v", a.qa.attachments[2])
	}
	for _, name := range []string{"photo.png", "ext.md", "Plan.md", "spec.pdf"} {
		if !tt.HasText(name) {
			t.Fatalf("missing attachment chip %s in %v", name, tt.Texts())
		}
	}

	// Clicking the space document chip opens that document; removing spec.pdf drops it.
	if err := tt.Click("打开附件：Plan.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc == nil || a.doc.rel != "Plan.md" {
		t.Fatalf("expected Plan.md to open, got %v", a.doc)
	}
	if err := tt.Click("移除附件：spec.pdf"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.qa.attachments) != 3 {
		t.Fatalf("expected 3 attachments after removal, got %d", len(a.qa.attachments))
	}

	a.open("Welcome.md")
	tt.Frame()
	if err := tt.Click("问题"); err != nil {
		t.Fatal(err)
	}
	tt.Type("结合图片和外部参考修改 Plan.md")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	turn := a.qa.qaSession(sp.ID).turns[0]
	qaSettle(t, tt, turn)

	if turn.err != nil {
		t.Fatalf("turn err = %v", turn.err)
	}
	if len(turn.attachments) != 3 {
		t.Fatalf("turn attachments missing: %+v", turn.attachments)
	}
	if turn.edit == nil || len(turn.edit.Candidates) == 0 || len(turn.edit.Candidates[0].Changes) == 0 {
		t.Fatalf("expected editable Plan.md changes in turn.edit: %+v", turn.edit)
	}
	ch := turn.edit.Candidates[0].Changes[0]
	if ch.Path != "Plan.md" || !strings.Contains(ch.Operation.Text, "更新后的计划") {
		t.Fatalf("unexpected edit change: %+v", ch)
	}

	// Verify conversation JSON does not store raw base64 data.
	convData, err := os.ReadFile(filepath.Join(a.qa.oneBotConversationDir(), a.qa.qaSession(sp.ID).id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(convData), "data:image/png;base64,") {
		t.Fatal("conversation history leaked inline base64 payload")
	}
	if !strings.Contains(string(convData), `"name": "photo.png"`) || !strings.Contains(string(convData), `"relPath": "Plan.md"`) {
		t.Fatalf("conversation history missing attachment badges: %s", convData)
	}
}

func TestOneBotSidebarDragFolderMultiSelectAndFavoriteDrop(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "Alpha.md", "# Alpha\n\nA"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Beta.md", "# Beta\n\nB"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	qaBox, ok := tt.Find("问题")
	if !ok {
		t.Fatal("OneBot input not found")
	}
	dstX, dstY := qaBox.X+20, qaBox.Y+20

	// Drag a favorite tile into OneBot.
	favRect, ok := tt.Find("Welcome")
	if !ok {
		t.Fatal("favorite Welcome not found")
	}
	tt.Press(favRect.X+favRect.W/2, favRect.Y+favRect.H/2)
	tt.Move(favRect.X+favRect.W/2+8, favRect.Y+favRect.H/2)
	tt.Frame()
	tt.Move(dstX, dstY)
	tt.Frame()
	if !a.qa.dropHover || !tt.HasText("松开以引用到 OneBot（支持多文件、图片）") {
		t.Fatalf("expected OneBot drop hover hint during favorite drag, texts=%v", tt.Texts())
	}
	tt.Release(dstX, dstY)
	tt.Frame()
	if len(a.qa.attachments) != 1 || a.qa.attachments[0].RelPath != "Welcome.md" {
		t.Fatalf("favorite drop failed: %+v", a.qa.attachments)
	}

	// Drag a folder row ("Journal") from sidebar into OneBot to expand its documents.
	folderRect, ok := tt.Find("Journal")
	if !ok {
		t.Fatal("Journal folder row not found")
	}
	tt.Press(folderRect.X+folderRect.W/2, folderRect.Y+folderRect.H/2)
	tt.Move(folderRect.X+folderRect.W/2+8, folderRect.Y+folderRect.H/2)
	tt.Frame()
	tt.Move(dstX, dstY)
	tt.Frame()
	if !a.qa.dropHover {
		t.Fatal("expected dropHover during sidebar folder drag")
	}
	tt.Release(dstX, dstY)
	tt.Frame()
	var rels []string
	for _, item := range a.qa.attachments {
		rels = append(rels, item.RelPath)
	}
	if !slices.Contains(rels, "Journal/Ideas.md") {
		t.Fatalf("folder drop did not expand Journal/Ideas.md: %v", rels)
	}

	// Multi-select Alpha.md + Beta.md and drag into OneBot.
	a.qa.attachments = nil
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	alphaRect, ok := tt.Find("Alpha")
	if !ok {
		t.Fatal("Alpha row not found")
	}
	tt.Press(alphaRect.X+alphaRect.W/2, alphaRect.Y+alphaRect.H/2)
	tt.Move(alphaRect.X+alphaRect.W/2+8, alphaRect.Y+alphaRect.H/2)
	tt.Frame()
	tt.Move(dstX, dstY)
	tt.Frame()
	tt.Release(dstX, dstY)
	tt.Frame()
	rels = rels[:0]
	for _, item := range a.qa.attachments {
		rels = append(rels, item.RelPath)
	}
	if len(rels) != 2 || !slices.Contains(rels, "Alpha.md") || !slices.Contains(rels, "Beta.md") {
		t.Fatalf("multi-select drag got %v", rels)
	}
}

func TestOneBotClipboardPasteImageFilesAndText(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		reply("收到附件。"),
	}}
	a, tt := qaApp(t, model)
	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	if err := tt.Click("问题"); err != nil {
		t.Fatal(err)
	}

	// Paste PNG image.
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBuf.Bytes()))))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if len(a.qa.attachments) != 1 || a.qa.attachments[0].Part.Name != "pasted-image-1.png" || a.qa.attachments[0].Thumb == nil {
		t.Fatalf("clipboard PNG paste failed: %+v", a.qa.attachments)
	}

	// Paste file list from clipboard.
	dir := t.TempDir()
	f1 := filepath.Join(dir, "one.txt")
	f2 := filepath.Join(dir, "two.txt")
	os.WriteFile(f1, []byte("first"), 0o600)
	os.WriteFile(f2, []byte("second"), 0o600)
	fileData, err := transfer.FileData(f1, f2)
	if err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(fileData)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if len(a.qa.attachments) != 3 {
		t.Fatalf("clipboard file list paste failed: %+v", a.qa.attachments)
	}

	// Plain text paste still inserts into the input box.
	tt.SetClipboardData(transfer.TextData("普通文本"))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if a.qa.input != "普通文本" {
		t.Fatalf("plain text paste = %q, want 普通文本", a.qa.input)
	}

	// Attachment-only send (clear text and send) works too.
	a.qa.input = ""
	tt.Frame()
	if err := tt.Click("发送 (Enter)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	sp := a.state.ActiveSpace()
	qaSettle(t, tt, a.qa.qaSession(sp.ID).turns[0])
	if len(model.reqs) != 1 || len(model.reqs[0].Messages[0].Parts) != 3 {
		t.Fatalf("attachment-only send failed: %+v", model.reqs)
	}
}
