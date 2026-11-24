package editor

// Behaviors adopted from upstream ArcMD v0.2.3-v0.2.5: keyboard keys, live
// inline HTML tags, literal copying and formula-aware table widths.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/pdf"
	"github.com/OUBIGFA/FreeFolio/internal/wiki"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestTableCellAndLiteralPlainTextCopy(t *testing.T) {
	md := strings.Join([]string{
		"| 服务 | 账号 | 密码 |",
		"| --- | :---: | ---: |",
		`| 微信 | admin_01 | Abc_123*456[7]\8<9>\$0 |`,
		"| 数据库 | `db_user` | `P@ss_w0rd*99` |",
		"| 后台 | **ops_user** | <span style=\"color:#D83931\" data-color=\"red\">Red_Pass*1</span> |",
	}, "\n")
	e, tt := newTester(md)
	focus(e, tt)

	wantPasswords := map[int]string{
		1: "Abc_123*456[7]\\8<9>$0",
		2: "P@ss_w0rd*99",
		3: "Red_Pass*1",
	}
	table := e.Doc.Blocks[0]
	if table.Kind != "table" || len(table.Children) != 4 {
		t.Fatalf("unexpected table blocks: %+v", e.Doc.Blocks)
	}
	for rowIdx, want := range wantPasswords {
		cell := table.Children[rowIdx].Children[2]
		leaf, ok := e.Engine.LeafByID(cell.ID)
		if !ok {
			t.Fatalf("missing leaf for row %d", rowIdx)
		}
		e.SetSelection(leaf.Start, leaf.End)
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != want {
			t.Fatalf("row %d plain clipboard = %q; want exact %q", rowIdx, got, want)
		}
		// Centered/right-aligned cell fragments must not leak <p align="..."> wrappers into Markdown.
		rawMD, err := tt.ClipboardData().Read(transfer.Markdown)
		if err != nil {
			t.Fatalf("row %d missing Markdown clipboard: %v", rowIdx, err)
		}
		if strings.Contains(string(rawMD), "<p ") || strings.Contains(string(rawMD), "</p>") {
			t.Fatalf("row %d Markdown clipboard leaked paragraph alignment HTML: %q", rowIdx, string(rawMD))
		}
	}

	// Partial multi-cell copy across a row uses tab-separated literal text without backslash escapes.
	c1, _ := e.Engine.LeafByID(table.Children[1].Children[1].ID)
	c2, _ := e.Engine.LeafByID(table.Children[1].Children[2].ID)
	e.SetSelection(c1.Start, c2.End)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "admin_01\tAbc_123*456[7]\\8<9>$0"; got != want {
		t.Fatalf("multi-cell plain clipboard = %q; want %q", got, want)
	}

	// Plain prose and inline code selections also copy literal text without Markdown escapes or backticks.
	e2, tt2 := newTester("- 密码：Pass_123*456[0]\n\n命令：`sk_live_123*abc`")
	focus(e2, tt2)
	listLeaf := e2.Doc.Leaves()[0]
	prefixLen := document.Len("密码：")
	e2.SetSelection(listLeaf.Start+prefixLen, listLeaf.End)
	tt2.Frame()
	tt2.Key(ui.Cmd, ui.KeyC)
	if got, want := tt2.Clipboard(), "Pass_123*456[0]"; got != want {
		t.Fatalf("list slice plain clipboard = %q; want %q", got, want)
	}

	codeLeaf := e2.Doc.Leaves()[1]
	codePrefix := document.Len("命令：")
	e2.SetSelection(codeLeaf.Start+codePrefix, codeLeaf.End)
	tt2.Frame()
	tt2.Key(ui.Cmd, ui.KeyC)
	if got, want := tt2.Clipboard(), "sk_live_123*abc"; got != want {
		t.Fatalf("inline code plain clipboard = %q; want %q", got, want)
	}

	// Pasting a password containing Markdown punctuation into a table cell preserves every character literally.
	emptyCellTable, tt3 := newTester("| 账号 | 密码 |\n| --- | --- |\n| root |  |")
	focus(emptyCellTable, tt3)
	targetCell, _ := emptyCellTable.Engine.LeafByID(emptyCellTable.Doc.Blocks[0].Children[1].Children[1].ID)
	emptyCellTable.SetSelection(targetCell.Start, targetCell.End)
	tt3.Frame()
	rawPass := "#Abc*123*xyz_456~789~"
	tt3.SetClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte(rawPass)),
		transfer.Bytes(transfer.Markdown, []byte(`\#Abc\*123\*xyz_456\~789\~`)),
	)))
	tt3.Key(ui.Cmd, ui.KeyV)
	if got := emptyCellTable.Doc.Blocks[0].Children[1].Children[1].Text(); got != rawPass {
		t.Fatalf("paste into table cell = %q; want %q", got, rawPass)
	}

	// Intraword underscores stay unescaped in exported Markdown while boundary emphasis underscores still escape.
	roundDoc := document.New()
	roundDoc.Blocks = []*document.Block{document.NewBlock("paragraph", "Abc_123 and _literal_")}
	exported := ExportMarkdown(roundDoc)
	if got, want := exported, "Abc_123 and \\_literal\\_\n"; got != want {
		t.Fatalf("ExportMarkdown underscore escaping = %q; want %q", got, want)
	}
	reimported := ImportMarkdown(exported)
	if got := reimported.Blocks[0].Text(); got != "Abc_123 and _literal_" || len(reimported.Blocks[0].Runs) != 1 || reimported.Blocks[0].Runs[0].Style != (document.Style{}) {
		t.Fatalf("roundtrip altered plain text: %+v", reimported.Blocks[0].Runs)
	}
}

func TestTableInlineFormulasAutoSizeAndExpandWithWindow(t *testing.T) {
	if err := loadMathFonts(); err != nil {
		t.Fatal(err)
	}
	md := strings.Join([]string{
		"| 个体 | 相对阶 | 名义输出方程 | 指令上限候选值 |",
		"| --- | --- | --- | --- |",
		`| 1 | 1 | $\dot{y}_1 = -0.6y_1 + a_1 u_1$ | 2.0 |`,
		`| 2 | 2 | $\ddot{y}_2 = -0.4y_2 - 0.8\dot{y}_2 + a_2 u_2$ | 2.5 |`,
		`| 3 | 3 | $y_3^{(3)} = -0.2y_3 - 0.6\dot{y}_3 - 0.9\ddot{y}_3 + a_3 u_3$ | 3.0 |`,
	}, "\n")
	e, _ := newTester(md)
	tblID := e.Doc.Blocks[0].ID
	d, ok := e.tableDecoration(tblID)
	if !ok || len(d.cols) != 5 {
		t.Fatalf("unexpected table decoration: %+v", d)
	}
	col0W := d.cols[1] - d.cols[0]
	col2W := d.cols[3] - d.cols[2]
	if col2W <= col0W*2 {
		t.Fatalf("formula column did not receive content-aware width: col0=%v col2=%v", col0W, col2W)
	}
	for _, r := range e.rows {
		if r.layout == nil {
			continue
		}
		for _, o := range r.layout.objects {
			if o.box != nil && o.scale < 0.999 {
				t.Fatalf("table formula scaled down (%v) despite ample table width: %+v", o.scale, d.cols)
			}
		}
	}

	// A multi-formula wide table expands past the 820 prose column when the window widens.
	wideMD := strings.Join([]string{
		"| 编号 | 状态动态方程 | 观测输出方程 | 鲁棒控制律 | 稳定性判据 |",
		"| --- | --- | --- | --- | --- |",
		`| 1 | $x_{k+1} = A_1 x_k + B_1 u_k + E_1 w_k + d_k$ | $y_k = C_1 x_k + D_1 u_k + F_1 v_k$ | $u_k = -K_1 \hat{x}_k + G_1 r_k - H_1 \hat{d}_k$ | $V(x_{k+1}) - V(x_k) \le -\alpha \|x_k\|^2$ |`,
	}, "\n")
	ew, ttw := newTester(wideMD)
	ttw.SetSize(960, 650)
	ttw.Frame()
	dNarrow, _ := ew.tableDecoration(ew.Doc.Blocks[0].ID)

	ttw.SetSize(1400, 650)
	ttw.Frame()
	dWide, _ := ew.tableDecoration(ew.Doc.Blocks[0].ID)
	if dWide.rect.W <= dNarrow.rect.W+100 {
		t.Fatalf("widening window did not expand wide formula table: narrow=%v wide=%v", dNarrow.rect.W, dWide.rect.W)
	}
}

func TestLiveInlineHTMLTagsAndRollback(t *testing.T) {
	// <kbd> compiles live, exits keycap style immediately after >, and rolls back on Backspace.
	e, tt := newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Ctrl</kbd>")
	if e.Text() != "Ctrl" {
		t.Fatalf("<kbd>Ctrl</kbd> did not compile cleanly: %q", e.Text())
	}
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 1 || !runs[0].Style.Kbd || runs[0].Style.Code {
		t.Fatalf("expected Kbd run without Code, got %+v", runs)
	}
	typeKeys(tt, "+K")
	if e.Text() != "Ctrl+K" {
		t.Fatalf("expected Ctrl+K after continuing to type, got %q", e.Text())
	}
	runs = e.Doc.Blocks[0].Runs
	if len(runs) != 2 || !runs[0].Style.Kbd || runs[1].Style.Kbd {
		t.Fatalf("typing after </kbd> leaked Kbd style: %+v", runs)
	}

	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Shift</kbd>")
	if e.Text() != "Shift" {
		t.Fatalf("expected Shift before rollback, got %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "<kbd>Shift</kbd>" {
		t.Fatalf("Backspace did not roll back <kbd>Shift</kbd>: %q", e.Text())
	}
	if len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Style.Kbd {
		t.Fatalf("rolled-back <kbd> should be plain text: %+v", e.Doc.Blocks[0].Runs)
	}

	// <u>, <sup>, <sub>, <mark>, <span style="color:red"> compile live.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, `<u>重点</u> x<sup>2</sup> H<sub>2</sub>O <mark>hi</mark> <span style="color:red">red</span>`)
	runs = e.Doc.Blocks[0].Runs
	var gotU, gotSup, gotSub, gotMark, gotRed bool
	for _, r := range runs {
		switch {
		case r.Text == "重点" && r.Style.Underline:
			gotU = true
		case r.Text == "2" && r.Style.Script == "sup":
			gotSup = true
		case r.Text == "2" && r.Style.Script == "sub":
			gotSub = true
		case r.Text == "hi" && r.Style.Highlight == "light-yellow":
			gotMark = true
		case r.Text == "red" && r.Style.Color == "red":
			gotRed = true
		}
	}
	if !gotU || !gotSup || !gotSub || !gotMark || !gotRed {
		t.Fatalf("missing expected live-compiled runs (u=%v sup=%v sub=%v mark=%v red=%v) in %+v",
			gotU, gotSup, gotSub, gotMark, gotRed, runs)
	}

	// <q>quote</q> compiles to typographic quotes and rolls back cleanly on Backspace.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<q>quote</q>")
	if e.Text() != "“quote”" {
		t.Fatalf("<q>quote</q> did not compile to smart quotes: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "<q>quote</q>" {
		t.Fatalf("Backspace did not roll back <q>quote</q>: %q", e.Text())
	}

	// <br> compiles to U+2028 in paragraphs (and rolls back) and to \n in table cells.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "line1<br>")
	if e.Text() != "line1\u2028" {
		t.Fatalf("<br> in paragraph got %q, want %q", e.Text(), "line1\u2028")
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "line1<br>" {
		t.Fatalf("Backspace did not roll back <br>: %q", e.Text())
	}
	e, tt = newTester("| H |\n| --- |\n| a |")
	focus(e, tt)
	e.SetSelection(5, 5) // inside second row cell "a"
	tt.Frame()
	typeKeys(tt, "<br/>b")
	table := e.Doc.Blocks[0]
	if table.Kind != "table" || len(table.Children) < 2 || len(table.Children[1].Children) == 0 {
		t.Fatalf("unexpected table structure: %+v", table)
	}
	cell := table.Children[1].Children[0]
	if cell.Kind != "cell" || cell.Text() != "a\nb" {
		t.Fatalf("<br/> in table cell got kind=%q text=%q, want cell %q", cell.Kind, cell.Text(), "a\nb")
	}

	// Empty-paragraph <hr> / <hr/> compiles to rule + trailing paragraph.
	for _, hr := range []string{"<hr>", "<hr/>"} {
		e, tt = newTester("")
		focus(e, tt)
		typeKeys(tt, hr)
		if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" || e.Doc.Blocks[1].Kind != "paragraph" {
			t.Fatalf("%s did not convert to rule+paragraph: %+v", hr, e.Doc.Blocks)
		}
	}

	// Non-triggers stay literal.
	for _, lit := range []string{"vector<int>", "a < b > c", "<span>plain</span>"} {
		e, tt = newTester("")
		focus(e, tt)
		typeKeys(tt, lit)
		if e.Text() != lit {
			t.Fatalf("non-trigger %q mutated into %q", lit, e.Text())
		}
	}

	// Repro: <kbd>Alt</kbd> -> Backspace rollback -> Ctrl+Z -> Enter -> typing "<" stays plain.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(0, ui.KeyEnter)
	tt.Type("<")
	if len(e.Doc.Blocks) != 2 || len(e.Doc.Blocks[1].Runs) != 1 || e.Doc.Blocks[1].Runs[0].Style.Kbd || e.Doc.Blocks[1].Text() != "<" {
		t.Fatalf("Enter after <kbd> rollback+undo leaked Kbd: %+v", e.Doc.Blocks)
	}

	// Deleting whole <kbd>Alt</kbd> span via Backspace resets Kbd so "<" is plain.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	for range 3 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("<")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd || runs[0].Text != "<" {
		t.Fatalf("deleting <kbd>Alt</kbd> leaked Kbd: %+v", runs)
	}

	// Pressing KeyRight at end of a trailing <kbd> span exits capsule style.
	e, tt = newTester("Press ")
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Frame()
	typeKeys(tt, "<kbd>Alt</kbd>")
	e.SetSelection(9, 9) // simulate clicking/navigating to end of keycap
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Type("<")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 3 || !runs[1].Style.Kbd || runs[2].Style.Kbd || runs[2].Text != "<" {
		t.Fatalf("KeyRight at end of <kbd> did not exit capsule style: %+v", runs)
	}
}

func TestWrapKbdAndVectorPDFKeycapAndComment(t *testing.T) {
	e, tt := newTester("Shortcut")
	focus(e, tt)
	e.SetSelection(0, len("Shortcut"))
	tt.Frame()
	e.Wrap("<kbd>")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || !runs[0].Style.Kbd || runs[0].Style.Code {
		t.Fatalf("Wrap(<kbd>) did not set Kbd: %+v", runs)
	}
	e.Wrap("<kbd>")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd {
		t.Fatalf("second Wrap(<kbd>) did not toggle Kbd off: %+v", runs)
	}

	doc := ImportMarkdown("Press <kbd>Ctrl</kbd> + <kbd>K</kbd>\n\n<!-- secret comment -->\n")
	e2 := FromOwnedDocument(doc)
	if len(doc.Blocks) != 2 || doc.Blocks[1].Kind != "raw" {
		t.Fatalf("expected paragraph + raw comment block, got %+v", doc.Blocks)
	}
	var tops []float32
	var opts Options
	ui.Render(func(c *ui.Context) {
		opts = Options{Width: 680, Theme: c.Theme()}
		tops = e2.Paginate(opts, 900)
	}, 680, 900, 1)
	if len(tops) == 0 {
		t.Fatal("expected at least one paginated PDF page")
	}
	if _, laid := e2.rowIDs[doc.Blocks[1].ID]; laid {
		t.Fatal("expected standalone HTML comment block to be omitted from print/PDF layout")
	}
	plainDoc := ImportMarkdown("Press <kbd>Ctrl</kbd> + <kbd>K</kbd>\n")
	ePlain := FromOwnedDocument(plainDoc)
	ui.Render(func(c *ui.Context) {
		ePlain.Paginate(Options{Width: 680, Theme: c.Theme()}, 900)
	}, 680, 900, 1)
	if e2.contentEnd != ePlain.contentEnd {
		t.Fatalf("omitted comment block left blank vertical gap in PDF layout: withComment=%v without=%v", e2.contentEnd, ePlain.contentEnd)
	}
	var out bytes.Buffer
	w := pdf.NewWriter(&out, "HTML & Keycap Test")
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e2.ExportVectorPage(vp, opts, tops[0], tops[0]+900, 0, len(tops), tops, 680, 980, 40)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("pdf writer close failed: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("expected valid PDF header, got %q", out.Bytes()[:min(16, out.Len())])
	}
}

func TestComprehensiveHTMLEditingWorkflow(t *testing.T) {
	// 1. Forward opening-tag compilation: rollback <kbd>Alt</kbd>, delete '>' of <kbd>, retype '>'.
	e, tt := newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace) // rolls back to "<kbd>Alt</kbd>" with caret at end of "<kbd>"? No, at end of "</kbd>".
	// Place caret after "<kbd" (position 4) after deleting '>' at position 5, then retype '>'.
	e.SetSelection(5, 5)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace) // "<kbdAlt</kbd>"
	tt.Type(">")
	if e.Text() != "Alt" || len(e.Doc.Blocks[0].Runs) != 1 || !e.Doc.Blocks[0].Runs[0].Style.Kbd {
		t.Fatalf("forward opening-tag compile failed on <kbd>: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// Changing <u>hi</u> to <mark>hi</mark> by editing opening tag triggers forward compile.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<u>hi</mark>") // unmatched closer stays literal
	e.SetSelection(0, 3)         // select "<u>"
	tt.Frame()
	typeKeys(tt, "<mark>") // delete selection and type opening tag up to '>'
	// Ensure <mark> compiles with </mark>
	if e.Text() != "hi" || len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Style.Highlight != "light-yellow" {
		t.Fatalf("forward <mark> compile failed: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// 2. Leading [Esc]退出: Home -> KeyLeft -> typing "按 " stays plain outside [Esc].
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Esc</kbd>退出")
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyLeft)
	tt.Type("按 ")
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 3 || runs[0].Text != "按 " || runs[0].Style.Kbd || runs[1].Text != "Esc" || !runs[1].Style.Kbd || runs[2].Text != "退出" || runs[2].Style.Kbd {
		t.Fatalf("Home + KeyLeft before leading <kbd> leaked Kbd: %+v", runs)
	}

	// 3. Interior "按 [Ctrl] 打开": move to right wall of Ctrl -> KeyRight -> "+K" stays plain.
	e, tt = newTester("按 Ctrl 打开")
	focus(e, tt)
	e.SetSelection(2, 6) // select "Ctrl"
	tt.Frame()
	e.Wrap("<kbd>")
	e.SetSelection(6, 6) // caret right at right edge of [Ctrl]
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Type("+K")
	runs = e.Doc.Blocks[0].Runs
	if len(runs) != 3 || runs[0].Text != "按 " || !runs[1].Style.Kbd || runs[1].Text != "Ctrl" || runs[2].Style.Kbd || runs[2].Text != "+K 打开" {
		t.Fatalf("interior KeyRight exit from <kbd> failed: %+v", runs)
	}

	// 4. Multi-block selection starting inside <kbd> -> Backspace -> typing "<" stays plain.
	e, tt = newTester("Press <kbd>Ctrl</kbd>\n\nSecond paragraph")
	focus(e, tt)
	e.SetSelection(6, e.Len()) // from inside "Ctrl" across block boundary to end
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Type("<")
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "Press <" {
		t.Fatalf("multi-block delete from <kbd> unexpected: %+v", e.Doc.Blocks)
	}
	if last := e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1]; last.Style.Kbd {
		t.Fatalf("multi-block delete from <kbd> leaked Kbd onto <: %+v", e.Doc.Blocks[0].Runs)
	}

	// 5. Special-character <kbd> Markdown round-trip.
	specials := []string{"⌘[", "[[x]]", "$x$", "*", "<", `\`}
	doc := ImportMarkdown("")
	for _, sp := range specials {
		_ = doc
		eng, engTT := newTester("")
		focus(eng, engTT)
		engTT.Type(sp)
		eng.SetSelection(0, eng.Len())
		engTT.Frame()
		eng.Wrap("<kbd>")
		md := ExportMarkdown(eng.Doc)
		reimported := ImportMarkdown(md)
		if got := reimported.PlainText(); got != sp {
			t.Fatalf("special kbd %q round-trip mismatch via %q: got %q", sp, md, got)
		}
		if len(reimported.Blocks) != 1 || len(reimported.Blocks[0].Runs) != 1 || !reimported.Blocks[0].Runs[0].Style.Kbd {
			t.Fatalf("special kbd %q lost Kbd style via %q: %+v", sp, md, reimported.Blocks)
		}
	}

	// 6. <font color="blue">蓝字</font> live compilation.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, `<font color="blue">蓝字</font>`)
	if len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Text != "蓝字" || e.Doc.Blocks[0].Runs[0].Style.Color != "blue" {
		t.Fatalf("<font color=blue> live compile failed: %+v", e.Doc.Blocks[0].Runs)
	}
}

func TestPostMergePR28Remediation(t *testing.T) {
	// 1. <br> and Markdown hard line breaks ("  \n") in prose import as soft
	// line breaks ("\u2028") and do not split into separate paragraphs on Enter.
	for _, md := range []string{"Line1<br>Line2\n", "Line1  \nLine2\n"} {
		e, tt := newTester(md)
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Key(0, ui.KeyEnter)
		typeKeys(tt, "Line3")
		if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "Line1\u2028Line2" || e.Doc.Blocks[1].Text() != "Line3" {
			t.Fatalf("prose <br> split paragraph on Enter for %q: %+v", md, e.Doc.Blocks)
		}
	}

	// 2. Live typing inline HTML comments in prose and list items hides them in
	// Block.Comments with Backspace rollback and unescaped Markdown export.
	{
		e, tt := newTester("")
		focus(e, tt)
		typeKeys(tt, "Hello <!-- secret -->")
		if e.Doc.Blocks[0].Text() != "Hello " || len(e.Doc.Blocks[0].Comments) != 1 || e.Doc.Blocks[0].Comments[0].Text != "<!-- secret -->" {
			t.Fatalf("inline comment did not compile live: runs=%+v comments=%+v", e.Doc.Blocks[0].Runs, e.Doc.Blocks[0].Comments)
		}
		tt.Key(0, ui.KeyBackspace)
		if e.Doc.Blocks[0].Text() != "Hello <!-- secret -->" || len(e.Doc.Blocks[0].Comments) != 0 {
			t.Fatalf("Backspace did not roll back live inline comment: text=%q comments=%+v", e.Doc.Blocks[0].Text(), e.Doc.Blocks[0].Comments)
		}
		// Re-type in a bullet list item and verify Markdown export stays unescaped.
		e2, tt2 := newTester("- ")
		focus(e2, tt2)
		e2.SetSelection(e2.Len(), e2.Len())
		typeKeys(tt2, "Item <!-- list-note --> done")
		exported := ExportMarkdown(e2.Doc)
		if strings.Contains(exported, "&lt;!--") || !strings.Contains(exported, "Item <!-- list-note --> done") {
			t.Fatalf("list inline comment escaped on export: %q", exported)
		}
	}

	// 3. Semantic inline HTML tags (<var>, <dfn>, <cite>, <samp>, <tt>, <small>, <abbr>) compile live.
	for _, tc := range []struct {
		input  string
		want   string
		italic bool
		code   bool
	}{
		{"<var>x</var>", "x", true, false},
		{"<dfn>term</dfn>", "term", true, false},
		{"<cite>book</cite>", "book", true, false},
		{"<samp>out</samp>", "out", false, true},
		{"<tt>mono</tt>", "mono", false, true},
		{"<small>note</small>", "note", false, false},
		{`<abbr title="HyperText">HTML</abbr>`, "HTML", false, false},
	} {
		e, tt := newTester("")
		focus(e, tt)
		typeKeys(tt, tc.input)
		if len(e.Doc.Blocks[0].Runs) != 1 {
			t.Fatalf("live tag %s runs=%+v", tc.input, e.Doc.Blocks[0].Runs)
		}
		r := e.Doc.Blocks[0].Runs[0]
		if r.Text != tc.want || r.Style.Italic != tc.italic || r.Style.Code != tc.code {
			t.Fatalf("live tag %s got run=%+v", tc.input, r)
		}
		if md := ExportMarkdown(e.Doc); strings.Contains(md, "&lt;") {
			t.Fatalf("live tag %s escaped on export: %q", tc.input, md)
		}
	}

	// 4 & 5. Standalone HTML comment cards reserve header space above text and
	// stay excluded from tags, wiki links, word count, and mind map nodes.
	{
		e, tt := newTester("正文两字\n\n<!-- " + strings.Repeat("长单行注释内容 ", 12) + "#secret-tag [[SecretDoc]] -->\n")
		tt.Frame()
		if got := e.wordCount(); got != 4 {
			t.Fatalf("standalone HTML comment counted toward words: got %d, want 4", got)
		}
		if tags := wiki.Tags(e.Doc.PlainText()); len(tags) != 0 {
			t.Fatalf("standalone HTML comment leaked tags: %+v", tags)
		}
		if links := wiki.Links(e.Doc.PlainText()); len(links) != 0 {
			t.Fatalf("standalone HTML comment leaked wiki links: %+v", links)
		}
		if !shouldSkipMapBlock(e.Doc.Blocks[1]) {
			t.Fatal("standalone HTML comment block should be skipped in mind map")
		}

	}

	// 6. Edge cases: <kbd>`Ctrl`</kbd> mutual exclusion, <div align="center"> skipping
	// code/math/rule, MindMap truncateMapRuns with Style.Kbd '$', and overlapping box+kbd padding.
	{
		kbdCode := ImportMarkdown("<kbd>`Ctrl`</kbd>\n")
		if r := kbdCode.Blocks[0].Runs[0]; !r.Style.Kbd || r.Style.Code {
			t.Fatalf("<kbd>`Ctrl`</kbd> should be Kbd without Code: %+v", r)
		}
		aligned := ImportMarkdown("<div align=\"center\">\n\n# Title\n\n```go\nfmt.Println(1)\n```\n\n$$\nx=1\n$$\n\n</div>\n")
		if len(aligned.Blocks) != 3 || aligned.Blocks[0].Align != "center" || aligned.Blocks[1].Align != "" || aligned.Blocks[2].Align != "" {
			t.Fatalf("code/math inside <div align=center> should remain unaligned: %+v", aligned.Blocks)
		}
		runs := []document.Run{
			{Text: "按 "},
			{Text: "$", Style: document.Style{Kbd: true}},
			{Text: " 触发公式，" + strings.Repeat("后文内容", 85)},
		}
		kept, clipped := truncateMapRuns(runs, mapMaxProseRunes, mapMaxProseLines)
		if !clipped || len(kept) < 3 || !strings.Contains(kept[2].Text, "后文内容") {
			t.Fatalf("truncateMapRuns truncated at <kbd>$</kbd>: %+v", kept)
		}
		both := []document.Run{{Text: "Ctrl", Style: document.Style{Kbd: true, Annotation: "box"}}}
		if pad := trailingPadAt(both, 4, 16); pad != 0.4*16 {
			t.Fatalf("overlapping box+kbd trailingPadAt got %v, want %v", pad, 0.4*16)
		}
	}
}

func TestRound2HTMLCommentTypingCaseAndAlignExport(t *testing.T) {
	// 1. Hand-typing <!-- ... --> preserves "--" and straight ASCII quotes, promotes to raw comment + next paragraph, and undoes in one Cmd+Z step.
	e, tt := newTester("")
	focus(e, tt)
	commentSrc := `<!-- 备注：检查 a < b 且含 "引号" -->`
	typeKeys(tt, commentSrc)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != commentSrc || !isHTMLCommentBlock(e.Doc.Blocks[0].Text()) {
		t.Fatalf("hand-typed comment block unexpected: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Caret != e.LeafAt(e.Caret).Start {
		t.Fatalf("expected focused trailing paragraph after comment promotion, got %+v caret=%d", e.Doc.Blocks, e.Caret)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "paragraph" || !strings.HasPrefix(e.Doc.Blocks[0].Text(), `<!-- 备注：检查 a < b 且含 "引号"`) {
		t.Fatalf("Cmd+Z did not undo comment promotion back to paragraph cleanly: %+v", e.Doc.Blocks)
	}

	// 2. Trailing raw comment block supports double-Enter exit into a new paragraph.
	e, tt = newTester("<!-- 尾部注释 -->\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	tt.Type("正文")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != "<!-- 尾部注释 -->" {
		t.Fatalf("double-Enter exit on raw block damaged comment block: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Doc.Blocks[1].Text() != "正文" {
		t.Fatalf("double-Enter did not focus new paragraph after raw block: %+v", e.Doc.Blocks)
	}

	// 3. Uppercase HTML tags work in both live typing and Markdown import.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<KBD>F1</KBD> <MARK>亮</MARK><BR>换")
	runs := e.Doc.Blocks[0].Runs
	if !runs[0].Style.Kbd || runs[0].Text != "F1" || !strings.Contains(e.Text(), "\u2028换") {
		t.Fatalf("uppercase inline live tags failed: %+v text=%q", runs, e.Text())
	}
	var gotMark bool
	for _, r := range runs {
		if strings.Contains(r.Text, "亮") && r.Style.Highlight == "light-yellow" {
			gotMark = true
		}
	}
	if !gotMark {
		t.Fatalf("uppercase <MARK> did not compile highlight: %+v", runs)
	}

	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<HR>")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" {
		t.Fatalf("uppercase <HR> failed: %+v", e.Doc.Blocks)
	}

	upperMD := ImportMarkdown("<DIV ALIGN=\"CENTER\">\n\n### 居中\n\n</DIV>\n\n<KBD>F1</KBD>+<KBD>&lt;</KBD>\n")
	if len(upperMD.Blocks) != 2 || upperMD.Blocks[0].Align != "center" || upperMD.Blocks[1].Runs[0].Text != "F1" || !upperMD.Blocks[1].Runs[0].Style.Kbd || upperMD.Blocks[1].Runs[2].Text != "<" || !upperMD.Blocks[1].Runs[2].Style.Kbd {
		t.Fatalf("uppercase ImportMarkdown failed: %+v", upperMD.Blocks)
	}

	// 4. ExportHTML preserves center/right block alignment and escapes <kbd>&lt;</kbd>.
	alignDoc := ImportMarkdown("<div align=\"center\">\n\n居中段落\n\n</div>\n\n<div align=\"right\">\n\n右对齐段落\n\n</div>\n\n<kbd>&lt;</kbd>\n")
	exportedHTML := ExportHTML(alignDoc)
	if !strings.Contains(exportedHTML, `<p style="text-align:center">居中段落</p>`) ||
		!strings.Contains(exportedHTML, `<p style="text-align:right">右对齐段落</p>`) ||
		!strings.Contains(exportedHTML, `<kbd>&lt;</kbd>`) {
		t.Fatalf("ExportHTML alignment or <kbd>&lt;</kbd> unexpected:\n%s", exportedHTML)
	}
}

func TestReview9FixesRegression(t *testing.T) {
	// 1. [P1] Columns containing a centered quote followed by a normal paragraph
	// must round-trip without shattering the columns block.
	quote := document.NewBlock("quote", "居中引用")
	quote.Align = "center"
	colsDoc := &document.Document{
		Blocks: []*document.Block{
			{
				ID:   document.NewID(),
				Kind: "columns",
				Children: []*document.Block{
					{
						ID:       document.NewID(),
						Kind:     "column",
						Children: []*document.Block{quote, document.NewBlock("paragraph", "普通段落")},
					},
					{
						ID:       document.NewID(),
						Kind:     "column",
						Children: []*document.Block{document.NewBlock("paragraph", "第二列")},
					},
				},
			},
		},
	}
	colsMD := ExportMarkdown(colsDoc)
	reCols := ImportMarkdown(colsMD)
	if len(reCols.Blocks) != 1 || reCols.Blocks[0].Kind != "columns" || len(reCols.Blocks[0].Children) != 2 {
		t.Fatalf("columns + centered quote shattered on reopen:\nmd:\n%s\nblocks: %+v", colsMD, reCols.Blocks)
	}
	col0 := reCols.Blocks[0].Children[0].Children
	if len(col0) != 2 || col0[0].Kind != "quote" || col0[0].Align != "center" || col0[0].Text() != "居中引用" ||
		col0[1].Kind != "paragraph" || col0[1].Align != "" || col0[1].Text() != "普通段落" {
		t.Fatalf("columns col0 content corrupted: %+v", col0)
	}

	// 2. [P1] Centered README image badge (<a><img></a>) must stay lossless raw HTML
	// instead of turning into an empty paragraph and dropping href/src on edit.
	badges := []string{
		"<div align=\"center\">\n  <a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a>\n</div>\n",
		"<div align=\"center\"><a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a></div>\n",
		"<p align=\"center\"><a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a></p>\n",
	}
	for _, badgeMD := range badges {
		bdoc := ImportMarkdown(badgeMD)
		if len(bdoc.Blocks) != 1 || bdoc.Blocks[0].Kind != "raw" ||
			!strings.Contains(bdoc.Blocks[0].Text(), "https://example.com/ci") ||
			!strings.Contains(bdoc.Blocks[0].Text(), "https://example.com/badge.svg") {
			t.Fatalf("centered badge lost raw HTML or URLs for %q: %+v", badgeMD, bdoc.Blocks)
		}
	}

	// 3. [P1] Editing visible text in a paragraph with an inline HTML comment
	// must preserve the comment in Markdown export while keeping it hidden in HTML export.
	e, tt := newTester("Before <!-- keep me --> after\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if got := ExportMarkdown(e.Doc); got != "Before <!-- keep me --> after!\n" {
		t.Fatalf("editing paragraph dropped inline comment: got %q", got)
	}
	if htmlOut := ExportHTML(e.Doc); strings.Contains(htmlOut, "keep me") {
		t.Fatalf("ExportHTML leaked inline comment: %s", htmlOut)
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatalf("Encode with inline comments failed: %v", err)
	}
	decoded, err := document.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode with inline comments failed: %v", err)
	}
	if got := ExportMarkdown(decoded); got != "Before <!-- keep me --> after!\n" {
		t.Fatalf("Decode round-trip lost inline comment: got %q", got)
	}
	// Undo keeps comment; inserting at start shifts comment offset cleanly.
	tt.Key(ui.Cmd, ui.KeyZ)
	e.SetSelection(0, 0)
	tt.Frame()
	tt.Type("X")
	if got := ExportMarkdown(e.Doc); got != "XBefore <!-- keep me --> after\n" {
		t.Fatalf("prefix edit misaligned inline comment: got %q", got)
	}
	// Comment inside bold + centered paragraph also survives edit & reopen.
	e, tt = newTester("<p align=\"center\">Hi <!-- c --> there</p>\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if got := ExportMarkdown(e.Doc); !strings.Contains(got, "<!-- c -->") || !strings.Contains(got, "there!") {
		t.Fatalf("centered paragraph edit lost inline comment: %q", got)
	}

	// 4. [P2] Rich HTML table with custom column widths preserves exact spaces.
	tbl := document.Table(2, 2)
	tbl.Widths = []float32{180, 180}
	tbl.Children[0].Children[0].Runs = []document.Run{{Text: "H1"}}
	tbl.Children[0].Children[1].Runs = []document.Run{{Text: "H2"}}
	tbl.Children[1].Children[0].Runs = []document.Run{{Text: "  A  B  "}}
	tbl.Children[1].Children[1].Runs = []document.Run{{Text: "plain"}}
	tblDoc := &document.Document{Blocks: []*document.Block{tbl}}
	tblMD := ExportMarkdown(tblDoc)
	reTbl := ImportMarkdown(tblMD)
	if len(reTbl.Blocks) != 1 || reTbl.Blocks[0].Kind != "table" {
		t.Fatalf("rich table reopen failed: %+v", reTbl.Blocks)
	}
	if got := reTbl.Blocks[0].Children[1].Children[0].Text(); got != "  A  B  " {
		t.Fatalf("rich table collapsed cell spaces: got %q want %q\nmd:\n%s", got, "  A  B  ", tblMD)
	}

	// 5. [P2] Enter preserves active Bold on the new paragraph while still exiting Kbd.
	e, tt = newTester("")
	focus(e, tt)
	e.Engine.Toggle("bold")
	typeKeys(tt, "first")
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "second")
	if len(e.Doc.Blocks) != 2 || !e.Doc.Blocks[0].Runs[0].Style.Bold ||
		!e.Doc.Blocks[1].Runs[0].Style.Bold || e.Doc.Blocks[1].Text() != "second" {
		t.Fatalf("Enter cleared active Bold style: %+v", e.Doc.Blocks)
	}
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyEnter)
	tt.Type("<")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Runs[0].Style.Kbd {
		t.Fatalf("Enter after <kbd> leaked Kbd into next paragraph: %+v", e.Doc.Blocks)
	}

	// 6. [P2] Hand-typing HTML comment keeps tags and ellipsis literal inside unclosed comment.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<!-- use <b>x</b> ... -->")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != "<!-- use <b>x</b> ... -->" {
		t.Fatalf("hand-typed comment mutated body: %+v", e.Doc.Blocks)
	}

	// 7. [P2] Second-line <kbd> keycap paints on line 2, not line 1.
	line2Doc := &document.Document{
		Blocks: []*document.Block{
			{
				ID:   document.NewID(),
				Kind: "paragraph",
				Runs: []document.Run{
					{Text: "X\u2028          "},
					{Text: "Ctrl", Style: document.Style{Kbd: true}},
				},
			},
		},
	}
	e2 := FromOwnedDocument(line2Doc)
	tt2 := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e2, Options{Width: w, Theme: c.Theme()}) }, 640, 320)
	tt2.Frame()
	if len(e2.rows) == 0 || e2.rows[0].layout == nil {
		t.Fatal("line2 keycap row did not lay out")
	}
	r0 := e2.rows[0]
	kbdStart := document.Len("X\u2028          ")
	kbdEnd := kbdStart + document.Len("Ctrl")
	rects := r0.layout.SelectionRects(ui.TextInputRange{Start: kbdStart, End: kbdEnd})
	if len(rects) == 0 || rects[0].Y < 10 {
		t.Fatalf("expected Ctrl on line 2 (Y >= 10), got rects=%+v", rects)
	}
	img := tt2.Image()
	bg := img.At(img.Bounds().Max.X-4, img.Bounds().Max.Y-4)
	sampleX := int(r0.x + rects[0].X + rects[0].W/2)
	line1Top := int(r0.y)
	line1Bot := int(r0.y + rects[0].Y - 2)
	for y := line1Top; y <= line1Bot; y++ {
		if img.At(sampleX, y) != bg {
			t.Fatalf("phantom keycap pixels drawn on line 1 at (%d,%d)", sampleX, y)
		}
	}
	line2Top := int(r0.y + rects[0].Y)
	line2Bot := int(r0.y + rects[0].Y + rects[0].H + 2)
	foundLine2 := false
	for y := line2Top; y <= line2Bot; y++ {
		if img.At(sampleX, y) != bg {
			foundLine2 = true
			break
		}
	}
	if !foundLine2 {
		t.Fatalf("keycap pixels missing on line 2 at x=%d y=[%d..%d]", sampleX, line2Top, line2Bot)
	}

	// 8. [P2] Clipboard paste of a single centered HTML paragraph preserves Align on empty doc,
	// without re-aligning when pasted into the middle of an existing paragraph.
	e, tt = newTester("")
	focus(e, tt)
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte(`<p style="text-align:center">居中</p>`)),
		transfer.Bytes(transfer.Text, []byte("居中")),
	)))
	tt.Key(ui.Cmd, ui.KeyV)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "居中" || e.Doc.Blocks[0].Align != "center" {
		t.Fatalf("clipboard paste of single centered paragraph lost align: %+v", e.Doc.Blocks)
	}
	e, tt = newTester("hello world")
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Frame()
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte(`<p style="text-align:center">居中</p>`)),
		transfer.Bytes(transfer.Text, []byte("居中")),
	)))
	tt.Key(ui.Cmd, ui.KeyV)
	if e.Doc.Blocks[0].Align != "" {
		t.Fatalf("mid-paragraph paste should not overwrite existing alignment: %+v", e.Doc.Blocks[0])
	}
}
