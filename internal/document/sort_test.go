package document

import (
	"strings"
	"testing"
)

func column(t *Block, col int) string {
	var out []string
	for _, r := range t.Children {
		out = append(out, r.Children[col].Text())
	}
	return strings.Join(out, ",")
}

func TestSortTable(t *testing.T) {
	table := DelimitedTable([][]string{{"名称", "数量"}, {"file 10", "3"}, {"File 2", "12"}, {"", "1,000"}, {"apple", ""}})
	d := New()
	d.Blocks = []*Block{table}
	e := NewEngine(d)
	e.SortTable(table.ID, 0, false)
	if got := column(e.Doc.Blocks[0], 0); got != "名称,apple,File 2,file 10," {
		t.Fatal(got)
	}
	e.SortTable(table.ID, 1, true)
	if got := column(e.Doc.Blocks[0], 1); got != "数量,1,000,12,3," {
		t.Fatal("numbers by value, empty last", got)
	}
	e.Undo()
	if got := column(e.Doc.Blocks[0], 0); got != "名称,apple,File 2,file 10," {
		t.Fatal("undo", got)
	}
	e.Doc.Blocks[0].Children[1].Children[0].ColSpan = 2
	e.Doc.Blocks[0].Children[1].Children[1].Runs = nil
	if e.Doc.CanSortTable(table.ID) {
		t.Fatal("merged cells keep their rows")
	}
}

func TestDelimitedTablePadsRaggedRows(t *testing.T) {
	table := DelimitedTable([][]string{{"a", "b", "c"}, {"1"}})
	if len(table.Children[1].Children) != 3 || table.Children[1].Children[0].Text() != "1" {
		t.Fatal("ragged")
	}
	d := New()
	d.Blocks = []*Block{table}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTableLineTools(t *testing.T) {
	table := DelimitedTable([][]string{{"a", "b"}, {"1", "2"}, {"3", "4"}})
	d := New()
	d.Blocks = []*Block{table}
	e := NewEngine(d)
	e.DuplicateTableLine(table.ID, true, 1)
	tb := e.Doc.Blocks[0]
	if got := column(tb, 0); got != "a,1,1,3" {
		t.Fatal("duplicate row", got)
	}
	if tb.Children[1].ID == tb.Children[2].ID || tb.Children[1].Children[0].ID == tb.Children[2].Children[0].ID {
		t.Fatal("a duplicate takes new IDs")
	}
	e.DuplicateTableLine(table.ID, false, 0)
	tb = e.Doc.Blocks[0]
	if len(tb.Children[0].Children) != 3 || column(tb, 1) != "a,1,1,3" {
		t.Fatal("duplicate column", column(tb, 1))
	}
	e.ClearCells(table.ID, CellRange{Row0: 1, Col0: 0, Row1: 1, Col1: 2})
	if got := column(e.Doc.Blocks[0], 0); got != "a,,1,3" {
		t.Fatal("clear row", got)
	}
	e.AlignCells(table.ID, CellRange{Row0: 0, Col0: 2, Row1: 3, Col1: 2}, "right")
	for _, row := range e.Doc.Blocks[0].Children {
		if row.Children[2].Align != "right" || row.Children[0].Align != "" {
			t.Fatal("align column")
		}
	}
	e.AlignCells(table.ID, CellRange{Row0: 0, Col0: 2, Row1: 3, Col1: 2}, "left")
	if e.Doc.Blocks[0].Children[0].Children[2].Align != "" {
		t.Fatal("left is the default, stored empty")
	}
	e.Undo()
	e.Undo()
	e.Undo()
	if got := column(e.Doc.Blocks[0], 0); got != "a,1,1,3" {
		t.Fatal("each tool is one undo step", got)
	}
}
