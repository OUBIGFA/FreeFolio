package formats

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustContain(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func convert(t *testing.T, name string, data []byte) string {
	t.Helper()
	md, err := Convert(name, data)
	if err != nil {
		t.Fatalf("Convert(%s): %v", name, err)
	}
	return md
}

func TestLookup(t *testing.T) {
	cases := map[string]Mode{
		"a.go": Source, "B.JSON": Source, "x.csv": Source, "dir/.env": None, "y.docx": Converted,
		"z.tar.gz": Converted, "n.md": None, "p.pdf": None, "q.unknown": None, "cfg.env": Source,
	}
	for name, want := range cases {
		if got := ModeOf(name); got != want {
			t.Errorf("ModeOf(%q) = %v, want %v", name, got, want)
		}
	}
	if f, _ := Lookup("main.rs"); f.Language != "rust" {
		t.Errorf("main.rs language = %q", f.Language)
	}
}

func TestPluginFormats(t *testing.T) {
	defer SetPluginFormats(nil)
	defer SetPluginConverter(nil)
	SetPluginFormats([]Format{{Ext: ".HWP", Name: "HWP", Plugin: "dev.example.hwp"}, {Ext: ".docx", Plugin: "dev.evil"}})
	if f, ok := Lookup("a.hwp"); !ok || f.Mode != Converted || f.Plugin != "dev.example.hwp" {
		t.Fatalf("hwp = %+v %v", f, ok)
	}
	if f, _ := Lookup("a.docx"); f.Plugin != "" {
		t.Fatal("a plugin took over a built-in format")
	}
	if _, err := Convert("a.hwp", []byte("x")); err == nil {
		t.Fatal("no converter should fail")
	}
	SetPluginConverter(func(f Format, name string, data []byte) (string, error) { return "# " + name, nil })
	if md := convert(t, "a.hwp", []byte("x")); md != "# a.hwp" {
		t.Fatalf("plugin md = %q", md)
	}
}

func TestConvertRefusesSource(t *testing.T) {
	if _, err := Convert("a.go", []byte("package a")); err == nil {
		t.Fatal("source formats do not convert")
	}
}

func TestMalformedDoesNotPanic(t *testing.T) {
	junk := []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1garbage-garbage-garbage")
	for _, e := range Extensions() {
		if f, _ := Lookup("x." + e); f.Mode != Converted {
			continue
		}
		Convert("x."+e, junk)
		Convert("x."+e, []byte("PK\x03\x04"))
		Convert("x."+e, nil)
	}
}

const docxRels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

func TestDOCX(t *testing.T) {
	doc := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>报告标题</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>粗体</w:t></w:r><w:r><w:t xml:space="preserve"> 与 </w:t></w:r><w:hyperlink r:id="rId9"><w:r><w:t>链接</w:t></w:r></w:hyperlink></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>第一项</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>第二项</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>2</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`
	docRels := `<Relationships><Relationship Id="rId9" Type="x/hyperlink" Target="https://example.com/a b" TargetMode="External"/></Relationships>`
	numbering := `<w:numbering xmlns:w="w"><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`
	data := zipOf(t, map[string]string{"_rels/.rels": docxRels, "word/document.xml": doc, "word/_rels/document.xml.rels": docRels, "word/numbering.xml": numbering})
	md := convert(t, "a.docx", data)
	mustContain(t, md, "# 报告标题", "**粗体** 与 [链接](https://example.com/a%20b)", "1. 第一项\n2. 第二项", "| A | B |", "| 1 | 2 |")
}

func TestXLSX(t *testing.T) {
	wb := `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="销售" sheetId="1" r:id="rId1"/></sheets></workbook>`
	wbRels := `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
	shared := `<sst><si><t>名称</t></si><si><t>日期</t></si><si><r><t>苹</t></r><r><t>果</t></r></si></sst>`
	styles := `<styleSheet><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`
	sheet := `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="inlineStr"><is><t>数量</t></is></c></row>
<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" s="1"><v>45658</v></c><c r="C2"><v>0.30000000000000004</v></c></row></sheetData></worksheet>`
	data := zipOf(t, map[string]string{"_rels/.rels": strings.ReplaceAll(docxRels, "word/document.xml", "xl/workbook.xml"), "xl/workbook.xml": wb, "xl/_rels/workbook.xml.rels": wbRels,
		"xl/sharedStrings.xml": shared, "xl/styles.xml": styles, "xl/worksheets/sheet1.xml": sheet})
	md := convert(t, "a.xlsx", data)
	mustContain(t, md, "## 销售", "| 名称 | 日期 | 数量 |", "| 苹果 | 2025-01-01 | 0.3 |")
}

func TestPPTX(t *testing.T) {
	pres := `<p:presentation xmlns:p="p" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`
	presRels := `<Relationships><Relationship Id="rId2" Type="x/slide" Target="slides/slide1.xml"/></Relationships>`
	slide := `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>年度回顾</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>增长 20%</a:t></a:r></a:p><a:p><a:pPr lvl="1"/><a:r><a:t>细节</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`
	slideRels := `<Relationships><Relationship Id="rId1" Type="x/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>`
	notes := `<p:notes xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>讲者备注</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`
	data := zipOf(t, map[string]string{"_rels/.rels": strings.ReplaceAll(docxRels, "word/document.xml", "ppt/presentation.xml"), "ppt/presentation.xml": pres,
		"ppt/_rels/presentation.xml.rels": presRels, "ppt/slides/slide1.xml": slide, "ppt/slides/_rels/slide1.xml.rels": slideRels, "ppt/notesSlides/notesSlide1.xml": notes})
	md := convert(t, "a.pptx", data)
	mustContain(t, md, "## 幻灯片 1 · 年度回顾", "- 增长 20%\n  - 细节", "讲者备注")
}

func TestODT(t *testing.T) {
	content := `<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"><office:body><office:text>
<text:h text:outline-level="2">章节</text:h><text:p>正文<text:s text:c="2"/>内容</text:p>
<text:list><text:list-item><text:p>条目</text:p></text:list-item></text:list>
<table:table table:name="T"><table:table-row><table:table-cell><text:p>x</text:p></table:table-cell><table:table-cell table:number-columns-repeated="2"><text:p>y</text:p></table:table-cell></table:table-row></table:table>
</office:text></office:body></office:document-content>`
	md := convert(t, "a.odt", zipOf(t, map[string]string{"content.xml": content}))
	mustContain(t, md, "## 章节", "正文  内容", "- 条目", "| x | y | y |")
}

func TestRTF(t *testing.T) {
	rtf := `{\rtf1\ansi\ansicpg936{\fonttbl{\f0\fcharset134 SimSun;}}{\*\generator x;}\f0 \b \'d6\'d0\'ce\'c4\b0  plain \u8364? end\par
\trowd\intbl a\cell b\cell\row\pard tail\par}`
	md := convert(t, "a.rtf", []byte(rtf))
	mustContain(t, md, "**中文** plain €", "| a | b |", "tail")
	if strings.Contains(md, "SimSun") || strings.Contains(md, "generator") {
		t.Errorf("destinations leaked: %s", md)
	}
}

func TestEML(t *testing.T) {
	eml := "From: =?UTF-8?B?5byg5LiJ?= <a@example.com>\r\nTo: b@example.com\r\nSubject: =?UTF-8?B?5Lya6K6u?=\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<p>=E4=BD=A0=E5=A5=BD <b>world</b></p>\r\n" +
		"--XX\r\nContent-Type: application/pdf; name=a.pdf\r\nContent-Disposition: attachment; filename=a.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERg==\r\n--XX--\r\n"
	md := convert(t, "a.eml", []byte(eml))
	mustContain(t, md, "# 会议", `张三 \<a@example.com\>`, "你好 **world**", "a.pdf (4 B)")
}

func TestICSAndVCF(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:周会\r\nDTSTART;TZID=Asia/Shanghai:20261124T100000\r\nLOCATION:3 楼\r\nDESCRIPTION:议程\\n第二行\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	mustContain(t, convert(t, "a.ics", []byte(ics)), "## 周会", "2026-11-24 10:00 (Asia/Shanghai)", "3 楼", "议程")
	vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:李四\r\nTEL;TYPE=CELL:+86 138\r\nEMAIL:li@example.com\r\nEND:VCARD\r\n"
	mustContain(t, convert(t, "a.vcf", []byte(vcf)), "## 李四", "+86 138", "li@example.com")
}

func TestEPUB(t *testing.T) {
	data := zipOf(t, map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/c.opf"/></rootfiles></container>`,
		"OEBPS/c.opf":            `<package><metadata><dc:title>书名</dc:title><dc:creator>作者甲</dc:creator></metadata><manifest><item id="c1" href="ch%201.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c1"/></spine></package>`,
		"OEBPS/ch 1.xhtml":       `<html><body><h1>第一章</h1><p>开头<i>斜体</i>。</p><ul><li>a</li><li>b</li></ul></body></html>`,
	})
	mustContain(t, convert(t, "a.epub", data), "# 书名", "作者甲", "# 第一章", "开头*斜体*。", "- a\n- b")
}

func TestMindMapsAndDrawio(t *testing.T) {
	xm := zipOf(t, map[string]string{"content.json": `[{"title":"S","rootTopic":{"title":"中心","children":{"attached":[{"title":"分支","children":{"attached":[{"title":"叶"}]}}]}}}]`})
	mustContain(t, convert(t, "a.xmind", xm), "### 中心", "- 分支\n  - 叶")
	mm := `<map><node TEXT="根"><node TEXT="子"/></node></map>`
	mustContain(t, convert(t, "a.mm", []byte(mm)), "# 根", "- 子")
	model := `<mxGraphModel><root><mxCell id="0"/><mxCell id="a" value="开始" vertex="1"/><mxCell id="b" value="&lt;b&gt;结束&lt;/b&gt;" vertex="1"/><mxCell id="e" edge="1" source="a" target="b" value="是"/></root></mxGraphModel>`
	var buf bytes.Buffer
	fw, _ := flate.NewWriter(&buf, flate.BestCompression)
	fw.Write([]byte(url.PathEscape(model)))
	fw.Close()
	drawio := `<mxfile><diagram name="流程">` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `</diagram></mxfile>`
	mustContain(t, convert(t, "a.drawio", []byte(drawio)), "## 流程", "- 开始", "- 结束", "开始 → 结束（是）")
}

func TestNotebookAndArchives(t *testing.T) {
	nb := `{"cells":[{"cell_type":"markdown","source":["# 标题"]},{"cell_type":"code","source":"print(1)","outputs":[{"output_type":"stream","text":["1\n"]}]}],"metadata":{"language_info":{"name":"python"}}}`
	mustContain(t, convert(t, "a.ipynb", []byte(nb)), "# 标题", "```python\nprint(1)\n```", "```text\n1\n```")
	mustContain(t, convert(t, "a.zip", zipOf(t, map[string]string{"dir/a.txt": "hello"})), "dir/a.txt", "5 B")
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	tw.WriteHeader(&tar.Header{Name: "x/y.md", Size: 3, Mode: 0o644})
	tw.Write([]byte("abc"))
	tw.Close()
	mustContain(t, convert(t, "a.tar", tb.Bytes()), "x/y.md", "3 B")
}

func TestPalmDecompress(t *testing.T) {
	// "abcabc": literals then a back reference of distance 3, length 3.
	in := []byte{'a', 'b', 'c', 0x80, 0x18, 0xC1}
	if got := string(palmDecompress(in)); got != "abcabc A" {
		t.Fatalf("palmDecompress = %q", got)
	}
}

func TestEscapeInline(t *testing.T) {
	for in, want := range map[string]string{"# x": `\# x`, "1. y": `1\. y`, "0.3": "0.3", "a*b": `a\*b`, "- z": `\- z`} {
		if got := escapeInline(in); got != want {
			t.Errorf("escapeInline(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSQLColumns(t *testing.T) {
	got := sqlColumns(`CREATE TABLE "t" (id INTEGER PRIMARY KEY, "name" TEXT, price NUMERIC(10,2), PRIMARY KEY(id))`)
	if strings.Join(got, ",") != "id,name,price" {
		t.Fatalf("columns = %v", got)
	}
}
