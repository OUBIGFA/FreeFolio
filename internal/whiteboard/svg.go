package whiteboard

import (
	"fmt"
	"html"
	"math"
	"strings"
)

type SVGOptions struct {
	EmbedScene   bool
	Background   bool
	Padding      float64
	ResolveImage func(*FileData) (string, error)
	FontCSS      string
}

func FontName(id int) string {
	switch id {
	case 1:
		return "Virgil"
	case 2, 9:
		return "Arial"
	case 3:
		return "Cascadia"
	case 6:
		return "Nunito"
	case 7:
		return "Lilita One"
	case 8:
		return "Comic Shanns"
	case 10:
		return "Assistant"
	default:
		return "Excalifont"
	}
}
func FontLineHeight(id int) float64 {
	switch id {
	case 2, 7, 9:
		return 1.15
	case 3:
		return 1.2
	default:
		return 1.25
	}
}
func TextBaseline(id int, size, lineHeight float64) float64 {
	u, a, d := float64(1000), float64(886), float64(-374)
	switch id {
	case 2:
		u, a, d = 2048, 1577, -471
	case 3:
		u, a, d = 2048, 1900, -480
	case 6:
		u, a, d = 1000, 1011, -353
	case 7:
		u, a, d = 1000, 923, -220
	case 8:
		u, a, d = 1000, 750, -250
	case 9:
		u, a, d = 2048, 1854, -434
	case 10:
		u, a, d = 2048, 1021, -287
	}
	em := size / u
	return em*a + (lineHeight-em*a+em*d)/2
}

func ExportSVG(s *Scene, o SVGOptions) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	b := SceneBounds(s)
	if b.Empty() {
		b = Box{0, 0, 320, 180}
	}
	pad := o.Padding
	if pad == 0 {
		pad = 20
	}
	b = b.Expand(pad)
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%g %g %g %g" width="%g" height="%g">`, b.X, b.Y, b.W, b.H, b.W, b.H)
	if o.EmbedScene {
		text, err := s.Encode()
		if err != nil {
			return "", err
		}
		payload, err := EncodeSVGPayload(text)
		if err != nil {
			return "", err
		}
		out.WriteString("<metadata>" + payload + "</metadata>")
	}
	if o.FontCSS != "" {
		out.WriteString("<defs><style>" + html.EscapeString(o.FontCSS) + "</style></defs>")
	}
	if o.Background {
		fmt.Fprintf(&out, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`, b.X, b.Y, b.W, b.H, html.EscapeString(s.AppState.ViewBackgroundColor))
	}
	for _, group := range s.GroupRegions() {
		r := group.Bounds
		fmt.Fprintf(&out, `<rect x="%g" y="%g" width="%g" height="%g" rx="16" fill="#687cab" fill-opacity="0.12" stroke="#687cab" stroke-opacity="0.25"/>`, r.X, r.Y, r.W, r.H)
		if group.Name != "" {
			fmt.Fprintf(&out, `<text x="%g" y="%g" font-family="sans-serif" font-size="15" fill="#536585">%s</text>`, r.X+24, r.Y+28, html.EscapeString(group.Name))
		}
	}
	for _, e := range s.Elements {
		if e.IsDeleted || !e.IsFrame() {
			continue
		}
		fmt.Fprintf(&out, `<defs><clipPath id="frame-%s"><rect x="%g" y="%g" width="%g" height="%g" transform="rotate(%g %g %g)"/></clipPath></defs>`, html.EscapeString(e.ID), e.X, e.Y, e.W, e.H, e.Angle*180/math.Pi, e.X+e.W/2, e.Y+e.H/2)
	}
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		if e.FrameID != "" {
			fmt.Fprintf(&out, `<g clip-path="url(#frame-%s)">`, html.EscapeString(e.FrameID))
		}
		fmt.Fprintf(&out, `<g transform="translate(%g %g) rotate(%g %g %g)">`, e.X, e.Y, e.Angle*180/math.Pi, e.W/2, e.H/2)
		for _, p := range ElementPrimitives(e) {
			fill, stroke := p.Fill, p.Stroke
			if fill == "" {
				fill = "none"
			}
			if stroke == "" {
				stroke = "none"
			}
			fmt.Fprintf(&out, `<path d="%s" fill="%s" stroke="%s" stroke-width="%g" opacity="%g" stroke-linecap="round" stroke-linejoin="round"`, PathData(p.Ops), html.EscapeString(fill), html.EscapeString(stroke), p.Width, p.Opacity)
			if len(p.Dash) > 0 {
				var dash []string
				for _, v := range p.Dash {
					dash = append(dash, fmt.Sprint(v))
				}
				fmt.Fprintf(&out, ` stroke-dasharray="%s"`, strings.Join(dash, " "))
			}
			out.WriteString("/>")
		}
		switch e.Kind {
		case KindImage:
			f := s.Files[e.FileID]
			if f == nil {
				return "", fmt.Errorf("whiteboard: missing image %s", e.FileID)
			}
			href := f.DataURL
			var err error
			if o.ResolveImage != nil {
				href, err = o.ResolveImage(f)
			}
			if err != nil {
				return "", err
			}
			if href == "" {
				return "", fmt.Errorf("whiteboard: missing image data %s", e.FileID)
			}
			x, y, w, h := float64(0), float64(0), e.W, e.H
			if c := e.Crop; c != nil {
				x = -c.X * e.W / c.Width
				y = -c.Y * e.H / c.Height
				w = c.NaturalWidth * e.W / c.Width
				h = c.NaturalHeight * e.H / c.Height
			}
			sx, sy := e.Scale[0], e.Scale[1]
			if sx == 0 {
				sx = 1
			}
			if sy == 0 {
				sy = 1
			}
			tx, ty := float64(0), float64(0)
			if sx < 0 {
				tx = e.W
			}
			if sy < 0 {
				ty = e.H
			}
			fmt.Fprintf(&out, `<defs><clipPath id="crop-%s"><rect width="%g" height="%g"/></clipPath></defs><g clip-path="url(#crop-%s)" opacity="%g"><g transform="translate(%g %g) scale(%g %g)"><image x="%g" y="%g" width="%g" height="%g" preserveAspectRatio="none" href="%s"/></g></g>`, html.EscapeString(e.ID), e.W, e.H, html.EscapeString(e.ID), e.Opacity/100, tx, ty, sx, sy, x, y, w, h, html.EscapeString(href))
		case KindText:
			if host := s.ElementByID(e.ContainerID); host != nil && host.Kind == KindArrow {
				fmt.Fprintf(&out, `<rect x="-4" y="-2" width="%g" height="%g" rx="2" fill="%s"/>`, e.W+8, e.H+4, html.EscapeString(s.AppState.ViewBackgroundColor))
			}
			lineHeight := e.LineHeight
			if lineHeight <= 0 {
				lineHeight = FontLineHeight(e.FontFamily)
			}
			lh := e.FontSize * lineHeight
			baseline := TextBaseline(e.FontFamily, e.FontSize, lh)
			anchor, x := "start", float64(0)
			if e.TextAlign == "center" {
				anchor = "middle"
				x = e.W / 2
			} else if e.TextAlign == "right" {
				anchor = "end"
				x = e.W
			}
			for i, line := range strings.Split(e.Text, "\n") {
				fmt.Fprintf(&out, `<text x="%g" y="%g" font-family="%s, Xiaolai, sans-serif" font-size="%g" text-anchor="%s" fill="%s" opacity="%g" xml:space="preserve">%s</text>`, x, baseline+float64(i)*lh, FontName(e.FontFamily), e.FontSize, anchor, html.EscapeString(e.StrokeColor), e.Opacity/100, html.EscapeString(line))
			}
		case KindFrame, KindMagicFrame:
			fmt.Fprintf(&out, `<rect width="%g" height="%g" fill="none" stroke="#adb5bd"/><text y="-8" font-family="sans-serif" font-size="14" fill="#868e96">%s</text>`, e.W, e.H, html.EscapeString(e.Name))
		}
		out.WriteString("</g>")
		if e.FrameID != "" {
			out.WriteString("</g>")
		}
	}
	out.WriteString("</svg>")
	return out.String(), nil
}
