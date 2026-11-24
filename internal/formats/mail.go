package formats

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/richardlehane/mscfb"
)

var wordDecoder = &mime.WordDecoder{CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(io.LimitReader(input, maxZipEntry))
	if err != nil {
		return nil, err
	}
	return strings.NewReader(decodeCharset(charset, b)), nil
}}

func decodeHeader(s string) string {
	if out, err := wordDecoder.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// mailPart is a leaf of a MIME message.
type mailPart struct {
	mediaType, charset, filename string
	body                         []byte
}

func convertEML(data []byte) (string, error) {
	msg, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return "", fmt.Errorf("formats: not a valid email: %w", err)
	}
	var w mdWriter
	subject := decodeHeader(msg.Header.Get("Subject"))
	if subject == "" {
		subject = i18n.T("（无主题）")
	}
	w.heading(1, subject)
	w.fields([][2]string{
		{i18n.T("发件人"), decodeHeader(msg.Header.Get("From"))},
		{i18n.T("收件人"), decodeHeader(msg.Header.Get("To"))},
		{i18n.T("抄送"), decodeHeader(msg.Header.Get("Cc"))},
		{i18n.T("日期"), msg.Header.Get("Date")},
	})
	var parts []mailPart
	collectParts(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Header.Get("Content-Disposition"), msg.Body, &parts, 0)
	writeMailBody(&w, parts)
	return w.String(), nil
}

func collectParts(ctype, cte, disp string, body io.Reader, out *[]mailPart, depth int) {
	if depth > 16 || len(*out) > 256 {
		return
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || mt == "" {
		mt, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			collectParts(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p.Header.Get("Content-Disposition"), p, out, depth+1)
		}
	}
	var r io.Reader = io.LimitReader(body, maxZipEntry)
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &spaceStripper{r: r})
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	b, _ := io.ReadAll(r)
	name := ""
	if _, dp, err := mime.ParseMediaType(disp); err == nil {
		name = decodeHeader(dp["filename"])
	}
	if name == "" {
		name = decodeHeader(params["name"])
	}
	*out = append(*out, mailPart{mt, params["charset"], name, b})
}

// spaceStripper drops the line breaks base64 bodies are folded with.
type spaceStripper struct{ r io.Reader }

func (s *spaceStripper) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for _, c := range p[:n] {
		if c != '\r' && c != '\n' && c != ' ' && c != '\t' {
			p[j] = c
			j++
		}
	}
	return j, err
}

func writeMailBody(w *mdWriter, parts []mailPart) {
	var plain, htmlBody *mailPart
	var attachments []string
	for i := range parts {
		p := &parts[i]
		switch {
		case p.filename != "":
			attachments = append(attachments, fmt.Sprintf("%s (%s)", p.filename, humanSize(len(p.body))))
		case p.mediaType == "text/plain" && plain == nil:
			plain = p
		case p.mediaType == "text/html" && htmlBody == nil:
			htmlBody = p
		}
	}
	switch {
	case htmlBody != nil:
		htmlMarkdown(w, decodeCharset(htmlBody.charset, htmlBody.body))
	case plain != nil:
		w.para(decodeCharset(plain.charset, plain.body))
	}
	if len(attachments) > 0 {
		w.heading(2, i18n.T("附件"))
		w.bullets(attachments)
	}
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// ---- Outlook .msg ----

func convertMSG(data []byte) (string, error) {
	if !bytes.HasPrefix(data, oleMagic) {
		return "", ErrUnsupported
	}
	r, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("formats: not a valid Outlook message: %w", err)
	}
	// Properties are streams named __substg1.0_TTTTPPPP: tag then type.
	props := map[string][]byte{}
	var attachments []string
	attachNames := map[string]string{}
	attachSizes := map[string]int{}
	for e, err := r.Next(); err == nil; e, err = r.Next() {
		if !strings.HasPrefix(e.Name, "__substg1.0_") || len(e.Name) < 20 || e.Size > maxZipEntry {
			continue
		}
		key := e.Name[12:20]
		dir := strings.Join(e.Path, "/")
		switch {
		case dir == "":
			if key[:4] == "1000" || key[:4] == "1013" || key[:4] == "0037" || key[:4] == "0C1A" || key[:4] == "0E04" || key[:4] == "0E03" || key[:4] == "0039" || key[:4] == "0C1F" || key[:4] == "1009" {
				b, _ := io.ReadAll(io.LimitReader(e, e.Size))
				props[key] = b
			}
		case strings.HasPrefix(dir, "__attach_version1.0_") && len(e.Path) == 1:
			switch key[:4] {
			case "3707", "3704":
				b, _ := io.ReadAll(io.LimitReader(e, min(e.Size, 4096)))
				if attachNames[dir] == "" || key[:4] == "3707" {
					attachNames[dir] = msgString(key, b)
				}
			case "3701":
				attachSizes[dir] = int(e.Size)
			}
		}
	}
	for dir, name := range attachNames {
		attachments = append(attachments, fmt.Sprintf("%s (%s)", name, humanSize(attachSizes[dir])))
	}
	sortNatural(attachments)
	get := func(tag string) string {
		for _, typ := range []string{"001F", "001E"} {
			if b, ok := props[tag+typ]; ok {
				return msgString(tag+typ, b)
			}
		}
		return ""
	}
	var w mdWriter
	subject := get("0037")
	if subject == "" {
		subject = i18n.T("（无主题）")
	}
	w.heading(1, subject)
	w.fields([][2]string{
		{i18n.T("发件人"), get("0C1A") + angle(get("0C1F"))},
		{i18n.T("收件人"), get("0E04")},
		{i18n.T("抄送"), get("0E03")},
	})
	switch {
	case props["10130102"] != nil:
		htmlMarkdown(&w, decodeText(props["10130102"]))
	case get("1013") != "":
		htmlMarkdown(&w, get("1013"))
	default:
		w.para(get("1000"))
	}
	if len(attachments) > 0 {
		w.heading(2, i18n.T("附件"))
		w.bullets(attachments)
	}
	return w.String(), nil
}

func angle(s string) string {
	if s == "" {
		return ""
	}
	return " <" + s + ">"
}

// msgString decodes a string property by its type: 001F is UTF-16, 001E
// the message's 8-bit code page.
func msgString(key string, b []byte) string {
	if strings.HasSuffix(key, "001F") {
		return strings.TrimRight(utf16LE(b), "\x00")
	}
	return strings.TrimRight(decodeText(b), "\x00")
}

// ---- iCalendar and vCard ----

// contentLines unfolds RFC 5545/6350 lines into name, parameters, value.
type contentLine struct {
	name   string
	params map[string]string
	value  string
}

func contentLines(data []byte) []contentLine {
	text := strings.ReplaceAll(decodeText(data), "\r\n", "\n")
	var raw []string
	for l := range strings.SplitSeq(text, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(raw) > 0 {
			raw[len(raw)-1] += l[1:]
			continue
		}
		raw = append(raw, l)
	}
	var out []contentLine
	for _, l := range raw {
		head, value, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		parts := strings.Split(head, ";")
		cl := contentLine{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: value}
		if i := strings.LastIndexByte(cl.name, '.'); i >= 0 {
			cl.name = cl.name[i+1:] // vCard groups: item1.EMAIL
		}
		for _, p := range parts[1:] {
			k, v, _ := strings.Cut(p, "=")
			cl.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
		if strings.EqualFold(cl.params["ENCODING"], "QUOTED-PRINTABLE") {
			if b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(cl.value))); err == nil {
				cl.value = decodeCharset(cl.params["CHARSET"], b)
			}
		}
		out = append(out, cl)
	}
	return out
}

func unescapeValue(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// icalTime shows a DATE or DATE-TIME value readably.
func icalTime(cl contentLine) string {
	v := strings.TrimSpace(cl.value)
	if len(v) >= 8 && strings.IndexFunc(v[:8], func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		out := v[:4] + "-" + v[4:6] + "-" + v[6:8]
		if len(v) >= 15 && v[8] == 'T' {
			out += " " + v[9:11] + ":" + v[11:13]
			if strings.HasSuffix(v, "Z") {
				out += " UTC"
			} else if tz := cl.params["TZID"]; tz != "" {
				out += " (" + tz + ")"
			}
		}
		return out
	}
	return v
}

func convertICS(data []byte) (string, error) {
	lines := contentLines(data)
	var w mdWriter
	var cur []contentLine
	inComp := ""
	count := 0
	for _, cl := range lines {
		switch cl.name {
		case "X-WR-CALNAME":
			if inComp == "" {
				w.heading(1, unescapeValue(cl.value))
			}
		case "BEGIN":
			switch v := strings.ToUpper(cl.value); v {
			case "VEVENT", "VTODO", "VJOURNAL":
				inComp, cur = v, nil
			}
		case "END":
			if strings.ToUpper(cl.value) == inComp && inComp != "" {
				count++
				writeEvent(&w, inComp, cur)
				inComp = ""
			}
		default:
			if inComp != "" {
				cur = append(cur, cl)
			}
		}
		if w.full() {
			break
		}
	}
	if count == 0 {
		w.para(i18n.T("（日历中没有日程）"))
	}
	return w.String(), nil
}

func writeEvent(w *mdWriter, kind string, props []contentLine) {
	first := func(name string) (contentLine, bool) {
		for _, p := range props {
			if p.name == name {
				return p, true
			}
		}
		return contentLine{}, false
	}
	title := i18n.T("（无标题）")
	if s, ok := first("SUMMARY"); ok && strings.TrimSpace(s.value) != "" {
		title = unescapeValue(s.value)
	}
	if kind == "VTODO" {
		title = i18n.T("待办：") + title
	}
	w.heading(2, title)
	var fields [][2]string
	if s, ok := first("DTSTART"); ok {
		fields = append(fields, [2]string{i18n.T("开始"), icalTime(s)})
	}
	if s, ok := first("DTEND"); ok {
		fields = append(fields, [2]string{i18n.T("结束"), icalTime(s)})
	}
	if s, ok := first("DUE"); ok {
		fields = append(fields, [2]string{i18n.T("截止"), icalTime(s)})
	}
	for _, k := range [][2]string{{"LOCATION", i18n.T("地点")}, {"ORGANIZER", i18n.T("组织者")}, {"RRULE", i18n.T("重复")}, {"STATUS", i18n.T("状态")}, {"URL", "URL"}} {
		if s, ok := first(k[0]); ok {
			fields = append(fields, [2]string{k[1], strings.TrimPrefix(strings.TrimPrefix(unescapeValue(s.value), "mailto:"), "MAILTO:")})
		}
	}
	var attendees []string
	for _, p := range props {
		if p.name == "ATTENDEE" {
			a := p.params["CN"]
			if a == "" {
				a = strings.TrimPrefix(strings.TrimPrefix(p.value, "mailto:"), "MAILTO:")
			}
			attendees = append(attendees, a)
		}
	}
	if len(attendees) > 0 {
		fields = append(fields, [2]string{i18n.T("参与者"), strings.Join(attendees, ", ")})
	}
	w.fields(fields)
	if s, ok := first("DESCRIPTION"); ok {
		w.para(unescapeValue(s.value))
	}
}

func convertVCF(data []byte) (string, error) {
	var w mdWriter
	var card []contentLine
	count := 0
	for _, cl := range contentLines(data) {
		switch {
		case cl.name == "BEGIN" && strings.EqualFold(cl.value, "VCARD"):
			card = nil
		case cl.name == "END" && strings.EqualFold(cl.value, "VCARD"):
			count++
			writeCard(&w, card)
		default:
			card = append(card, cl)
		}
		if w.full() {
			break
		}
	}
	if count == 0 {
		return "", ErrUnsupported
	}
	return w.String(), nil
}

func writeCard(w *mdWriter, card []contentLine) {
	name := ""
	var fields [][2]string
	labels := map[string]string{
		"TEL": i18n.T("电话"), "EMAIL": i18n.T("邮箱"), "ORG": i18n.T("单位"), "TITLE": i18n.T("职位"),
		"ADR": i18n.T("地址"), "URL": "URL", "BDAY": i18n.T("生日"), "NOTE": i18n.T("备注"), "NICKNAME": i18n.T("昵称"),
	}
	for _, cl := range card {
		switch cl.name {
		case "FN":
			name = unescapeValue(cl.value)
		case "N":
			if name == "" {
				parts := strings.Split(cl.value, ";")
				if len(parts) > 1 {
					name = strings.TrimSpace(parts[1] + " " + parts[0])
				} else {
					name = cl.value
				}
			}
		default:
			label, ok := labels[cl.name]
			if !ok {
				continue
			}
			v := unescapeValue(cl.value)
			if cl.name == "ADR" || cl.name == "ORG" {
				var keep []string
				for p := range strings.SplitSeq(cl.value, ";") {
					if p = strings.TrimSpace(unescapeValue(p)); p != "" {
						keep = append(keep, p)
					}
				}
				v = strings.Join(keep, ", ")
			}
			if t := cl.params["TYPE"]; t != "" && (cl.name == "TEL" || cl.name == "EMAIL" || cl.name == "ADR") {
				label += " (" + strings.ToLower(t) + ")"
			}
			fields = append(fields, [2]string{label, strings.TrimPrefix(v, "tel:")})
		}
	}
	if name == "" {
		name = i18n.T("（未命名联系人）")
	}
	w.heading(2, name)
	w.fields(fields)
}
