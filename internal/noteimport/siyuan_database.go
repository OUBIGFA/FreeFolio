package noteimport

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

func object(v any) map[string]any { o, _ := v.(map[string]any); return o }
func array(v any) []any           { a, _ := v.([]any); return a }
func stringValue(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(v)
	}
	return ""
}

// Attribute views become static tables, in their visible field and row order.
// The report explicitly distinguishes imported data from live database behavior.
func (c *syConverter) database(n syNode) string {
	id := n.s("AttributeViewID")
	var raw []byte
	for _, file := range c.s.files {
		if strings.HasSuffix(file, "storage/av/"+id+".json") {
			raw, _ = c.s.read(file)
			break
		}
	}
	var db map[string]any
	if json.Unmarshal(raw, &db) != nil {
		c.issue("missing-database", id)
		return inlineCode(id) + "\n\n"
	}
	keys := array(db["keyValues"])
	if len(keys) == 0 {
		c.issue("missing-database", id)
		return ""
	}
	viewID := n.prop("custom-sy-av-view")
	if viewID == "" {
		viewID = stringValue(db["viewID"])
	}
	var view map[string]any
	for i, v := range array(db["views"]) {
		o := object(v)
		if i == 0 || stringValue(o["id"]) == viewID {
			view = o
		}
		if stringValue(o["id"]) == viewID {
			break
		}
	}
	var layout map[string]any
	for _, v := range view {
		o := object(v)
		if o["columns"] != nil || o["fields"] != nil {
			layout = o
			break
		}
	}
	var columns []map[string]any
	visible := array(layout["columns"])
	if visible == nil {
		visible = array(layout["fields"])
	}
	if visible != nil {
		for _, field := range visible {
			f := object(field)
			if f["hidden"] == true {
				continue
			}
			for _, k := range keys {
				kv := object(k)
				if object(kv["key"])["id"] == f["id"] {
					columns = append(columns, kv)
					break
				}
			}
		}
	} else {
		for _, k := range keys {
			columns = append(columns, object(k))
		}
	}
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	order := array(layout["rowIds"])
	if order == nil {
		order = array(layout["itemIds"])
	}
	for _, v := range order {
		add(stringValue(v))
	}
	primary := object(keys[0])
	for _, k := range keys {
		if object(object(k)["key"])["type"] == "block" {
			primary = object(k)
			break
		}
	}
	for _, v := range array(primary["values"]) {
		add(stringValue(object(v)["blockID"]))
	}
	var header []string
	for _, col := range columns {
		header = append(header, stringValue(object(col["key"])["name"]))
	}
	rows := [][]string{header}
	for i, id := range ids {
		var row []string
		for _, col := range columns {
			kind := stringValue(object(col["key"])["type"])
			value := ""
			if kind == "lineNumber" {
				value = fmt.Sprint(i + 1)
			} else {
				for _, v := range array(col["values"]) {
					o := object(v)
					if o["blockID"] == id {
						value = c.databaseValue(o, kind)
						break
					}
				}
			}
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	name := stringValue(db["name"])
	c.issue("static-database", name+" ("+id+"): imported as a static table")
	return "**" + escape(name) + "**\n\n" + markdownTable(rows)
}

func (c *syConverter) databaseValue(v map[string]any, kind string) string {
	o := object(v[kind])
	content := stringValue(o["content"])
	switch kind {
	case "block":
		if content == "" {
			if target, ok := c.index[stringValue(o["id"])]; ok {
				return c.pages[target.page].title
			}
		}
		return content
	case "number":
		if o["isNotEmpty"] == false {
			return ""
		}
		if s := stringValue(o["formattedContent"]); s != "" {
			return s
		}
		return content
	case "checkbox":
		if o["checked"] == true {
			return "☑"
		}
		return "☐"
	case "date", "created", "updated":
		if o["isNotEmpty"] == false && kind == "date" {
			return ""
		}
		format := "2006-01-02 15:04"
		if o["isNotTime"] == true {
			format = "2006-01-02"
		}
		ms, ok := o["content"].(float64)
		if !ok {
			return content
		}
		s := time.UnixMilli(int64(ms)).Format(format)
		if o["hasEndDate"] == true {
			if end, ok := o["content2"].(float64); ok {
				s += " → " + time.UnixMilli(int64(end)).Format(format)
			}
		}
		return s
	case "select", "mSelect", "mAsset":
		key := "mSelect"
		if kind == "mAsset" {
			key = kind
		}
		var values []string
		for _, v := range array(v[key]) {
			o := object(v)
			s := stringValue(o["content"])
			if kind == "mAsset" {
				src := stringValue(o["content"])
				if src == "" {
					src = stringValue(o["path"])
				}
				if src != "" {
					s = "[" + escape(path.Base(src)) + "](" + c.asset(src) + ")"
				}
			}
			if s != "" {
				values = append(values, s)
			}
		}
		return strings.Join(values, ", ")
	case "relation":
		var values []string
		for _, v := range array(o["contents"]) {
			s := stringValue(object(object(v)["block"])["content"])
			if s != "" {
				values = append(values, s)
			}
		}
		if len(values) == 0 {
			for _, id := range array(o["blockIDs"]) {
				values = append(values, c.reference(stringValue(id), ""))
			}
		}
		return strings.Join(values, ", ")
	case "rollup":
		var values []string
		for _, v := range array(o["contents"]) {
			o := object(v)
			values = append(values, c.databaseValue(o, stringValue(o["type"])))
		}
		return strings.Join(values, ", ")
	default:
		return content
	}
}
