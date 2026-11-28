package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestDatabaseVersionRestorePreservesRecordsAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	db, rel := seedAppDatabase(t, a, "Roadmap")
	record := "# Alpha\n\nKeep the current record body\n"
	if err := vault.Write(root(a), "Roadmap/alpha.md", record); err != nil {
		t.Fatal(err)
	}
	older := db.Clone()
	older.Title = "Older roadmap"
	raw, err := database.EncodeDatabaseMarkdown(older, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := history.SaveMilestone(root(a), rel, raw, "Older schema", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(rel)
	a.versions.show()
	settle(tt)
	if a.versions.dbText != raw {
		t.Fatal("database version was not loaded for preview")
	}
	if err := tt.Click("恢复此版本"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.versions.open || a.doc.dbView.Store.Database().Title != older.Title {
		t.Fatal("restore did not apply the older schema")
	}
	if got, err := vault.Read(root(a), "Roadmap/alpha.md"); err != nil || got != record {
		t.Fatalf("restoring schema changed record contents: %q %v", got, err)
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.doc.dbView.Store.Database().Title != db.Title {
		t.Fatal("database restore could not be undone from the toolbar")
	}
}

func TestVersionGraphemeDiffAndMilestones(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "Roadmap.arcmd", "# 发布计划\n\n第一阶段完成 核心引擎 编写与自测。"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Roadmap.arcmd")
	tt.Frame()

	// Edit a few characters inline to trigger intra-line grapheme spans.
	caretAtEnd(a, tt)
	for range len([]rune(" 编写与自测。")) {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type(" 发布与验收。")
	saveSettle(tt)

	a.versions.show()
	tt.Frame()
	if !a.versions.open || len(a.versions.versions) < 2 {
		t.Fatalf("expected versions modal open with >= 2 versions, got %+v", a.versions)
	}
	a.versions.selectVersion(1)
	a.versions.showDiff = true // The original text-difference view remains available.
	tt.Frame()

	hasSpans := false
	for _, l := range a.versions.diff {
		if len(l.Spans) > 0 {
			hasSpans = true
			break
		}
	}
	if !hasSpans {
		t.Fatalf("expected intra-line grapheme Spans on diff lines, got %+v", a.versions.diff)
	}
	if !tt.HasText("编写与自测") || !tt.HasText("发布与验收") {
		t.Fatalf("expected grapheme diff fragments in rendered modal: %q", tt.Texts())
	}

	// Create a named milestone from the modal bar.
	if err := tt.Click("保存里程碑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("定稿 v1")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	vs, err := history.List(root(a), "Roadmap.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	foundLabel := false
	for _, ver := range vs {
		if ver.Label == "定稿 v1" && ver.Kept {
			foundLabel = true
		}
	}
	if !foundLabel {
		t.Fatalf("milestone not saved to labels.json: %+v", vs)
	}
	if !tt.HasText("定稿 v1") {
		t.Fatalf("milestone badge not visible in version list: %q", tt.Texts())
	}

	// Filter to milestones only.
	if err := tt.Click("里程碑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.versions.milestonesOnly {
		t.Fatal("expected milestonesOnly filter active")
	}

	// Rename the selected milestone inline via the action button.
	if err := tt.Click("命名里程碑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.versions.renamingFile == "" {
		t.Fatal("expected inline rename input to open")
	}
	for range len([]rune("定稿 v1")) {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("归档 v2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if !tt.HasText("归档 v2") {
		t.Fatalf("expected renamed milestone in UI: %q", tt.Texts())
	}
	labelsPath := filepath.Join(root(a), history.Dir, "Roadmap.arcmd", "labels.json")
	data, err := os.ReadFile(labelsPath)
	if err != nil || !strings.Contains(string(data), "归档 v2") {
		t.Fatalf("expected labels.json to contain renamed milestone, err=%v data=%q", err, string(data))
	}
}
