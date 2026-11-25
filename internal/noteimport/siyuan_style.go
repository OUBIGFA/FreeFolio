package noteimport

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var syColorVar = regexp.MustCompile(`var\(--b3-font-(color|background)([0-9]+)\)`)

// Keep text styling using literal CSS values understood by FreeFolio's native
// Markdown reader. SiYuan theme variables otherwise have no meaning outside it.
func syStyle(style string) string {
	font := []string{"#D83931", "#DE7802", "#245BDB", "#2EA121", "#8F959E", "#245BDB", "#DE7802", "#D83931", "#6425D0", "#245BDB", "#2EA121", "#DC9B04", ""}
	back := []string{"#FDE2E2", "#FCEAD5", "#E1EAFF", "#DFF0D8", "#F2F3F5", "#E1EAFF", "#FCEAD5", "#FDE2E2", "#EDE2FF", "#E1EAFF", "#DFF0D8", "#FFF3C2", "#DEE0E3"}
	var out []string
	for _, part := range strings.Split(style, ";") {
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "color", "background-color":
			value = syColorVar.ReplaceAllStringFunc(value, func(s string) string {
				m := syColorVar.FindStringSubmatch(s)
				i, _ := strconv.Atoi(m[2])
				if i < 1 || i > 13 {
					return ""
				}
				if m[1] == "color" {
					return font[i-1]
				}
				return back[i-1]
			})
			if strings.HasPrefix(value, "#") || strings.HasPrefix(value, "rgb(") || strings.HasPrefix(value, "rgba(") {
				out = append(out, key+":"+value)
			}
		case "text-align":
			if value == "left" || value == "center" || value == "right" {
				out = append(out, key+":"+value)
			}
		case "width":
			if n, e := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64); e == nil && n > 0 && n < 10000 {
				out = append(out, "width:"+strconv.FormatFloat(n, 'f', -1, 64)+"px")
			}
		}
	}
	return strings.Join(out, ";")
}

func styleAttr(n syNode) string {
	if style := syStyle(n.prop("style")); style != "" {
		return ` style="` + html.EscapeString(style) + `"`
	}
	return ""
}
