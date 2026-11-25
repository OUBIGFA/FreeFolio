package texttools

import (
	"errors"
	"testing"
)

func TestLineTools(t *testing.T) {
	cases := []struct {
		name, in, want string
		fn             func(string) string
	}{
		{"sort", "b\na\nc\n", "a\nb\nc\n", func(s string) string { return SortLines(s, false) }},
		{"sort descending", "b\na\nc", "c\nb\na", func(s string) string { return SortLines(s, true) }},
		{"unique", "a\nb\na\n\n\nb\n", "a\nb\n\n", UniqueLines},
		{"blank", "a\n \t\n\nb\n", "a\nb\n", RemoveBlankLines},
		{"trailing", "a  \nb\t\n c ", "a\nb\n c", TrimTrailingSpace},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestJSON(t *testing.T) {
	got, err := FormatJSON(`{"b":1,"a":[1.50,true]}`)
	if err != nil || got != "{\n  \"b\": 1,\n  \"a\": [\n    1.50,\n    true\n  ]\n}\n" {
		t.Fatalf("format %q, %v", got, err)
	}
	if got, err := CompactJSON("{\n  \"a\": 1\n}\n"); err != nil || got != "{\"a\":1}\n" {
		t.Fatalf("compact %q, %v", got, err)
	}
	err = CheckJSON("{\n  \"a\": 1,\n  \"b\": x\n}")
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 3 || se.Column != 8 {
		t.Fatalf("check %#v", err)
	}
	n, err := CheckJSONLines("{\"a\":1}\n\n[2]\n")
	if n != 2 || err != nil {
		t.Fatalf("lines %d, %v", n, err)
	}
	if _, err := CheckJSONLines("1\n{x}\n"); !errors.As(err, &se) || se.Line != 2 {
		t.Fatalf("bad line %v", err)
	}
}

func TestXMLAndYAML(t *testing.T) {
	if err := CheckXML(`<?xml version="1.0" encoding="GBK"?><a><b/></a>`); err != nil {
		t.Fatal(err)
	}
	var se *SyntaxError
	if err := CheckXML("<a>\n<b>\n</a>"); !errors.As(err, &se) || se.Line != 3 {
		t.Fatalf("xml %v", err)
	}
	if err := CheckXML("just text"); err == nil {
		t.Fatal("text without an element passed")
	}
	if err := CheckYAML("a: 1\n---\nb: [1, 2]\n"); err != nil {
		t.Fatal(err)
	}
	if err := CheckYAML("a: 1\n b: 2\nc: [\n"); !errors.As(err, &se) || se.Line == 0 {
		t.Fatalf("yaml %v", err)
	}
}

func TestFormatGo(t *testing.T) {
	got, err := FormatGo("package a\nfunc f( ){x:=1;_=x}\n")
	if err != nil || got != "package a\n\nfunc f() { x := 1; _ = x }\n" {
		t.Fatalf("%q, %v", got, err)
	}
	var se *SyntaxError
	if _, err := FormatGo("package a\nfunc {\n"); !errors.As(err, &se) || se.Line != 2 {
		t.Fatalf("%v", err)
	}
}

func TestDelimited(t *testing.T) {
	if Separator("a.tsv", "x,y") != '\t' || Separator("a.csv", "x;y;z,w\n1") != ';' || Separator("a.csv", "x") != ',' {
		t.Fatal("separator")
	}
	rows, err := ParseDelimited("a,b\n\"x\ny\",2\n\n3\n", ',')
	if err != nil || len(rows) != 3 || rows[1].Fields[0] != "x\ny" || rows[2].Line != 5 {
		t.Fatalf("%+v, %v", rows, err)
	}
	if r := RaggedRecord(rows); r == nil || r.Line != 5 {
		t.Fatalf("ragged %+v", r)
	}
}
