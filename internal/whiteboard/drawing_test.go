package whiteboard

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestFreehandPressureAndShortStrokeCaps(t *testing.T) {
	e := NewElement(KindFreedraw, 0, 0, 100, 0)
	e.StrokeWidth = 10
	e.Points = nil
	for i := 0; i <= 20; i++ {
		e.Points = append(e.Points, Point{float64(i) * 5, 0})
	}
	variable := FreehandOutline(e)
	e.StrokeOptions.Variability = "constant"
	constant := FreehandOutline(e)
	if !pointInPolygon(Point{50, 20}, variable) || pointInPolygon(Point{50, 20}, constant) || !pointInPolygon(Point{50, 10}, constant) {
		t.Fatal("pressure/constant stroke widths do not match source behavior")
	}
	e.StrokeWidth = 1
	e.Points = []Point{{0, 0}, {.01, 0}, {2.9, 0}}
	e.Pressures = []float64{1, 1, 1}
	e.SimulatePressure = false
	e.StrokeOptions.Variability = "variable"
	if !pointInPolygon(Point{.1, 4.9}, FreehandOutline(e)) {
		t.Fatal("short stroke cap missing")
	}
}

func TestSVGKeepsImageCropRotationFrameAndEditableScene(t *testing.T) {
	s := NewScene()
	frame := NewElement(KindFrame, 0, 0, 300, 200)
	frame.Name = "section"
	img := NewElement(KindImage, 20, 30, 160, 90)
	img.Angle = .2
	img.Scale = Point{-1, 1}
	img.FrameID = frame.ID
	img.FileID = "f"
	img.Crop = &ImageCrop{X: 10, Y: 5, Width: 320, Height: 180, NaturalWidth: 640, NaturalHeight: 360}
	s.Files["f"] = &FileData{ID: "f", MIMEType: "image/png", DataURL: "data:image/png;base64,iVBORw0KGgo="}
	text := NewElement(KindText, 20, 150, 150, 25)
	text.Text = "<API> & 中文"
	text.OriginalText = text.Text
	s.Elements = []*Element{frame, img, text}
	svg, err := ExportSVG(s, SVGOptions{EmbedScene: true})
	if err != nil {
		t.Fatal(err)
	}
	d := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() != "EOF" {
				t.Fatal(err)
			}
			break
		}
	}
	for _, part := range []string{"clipPath", "rotate(", "scale(-1 1)", "data:image/png;base64,", "&lt;API&gt; &amp; 中文", "payload-start"} {
		if !strings.Contains(svg, part) {
			t.Fatalf("missing %s", part)
		}
	}
	loaded, err := Decode(svg)
	if err != nil || loaded.ElementByID(img.ID).Crop.Width != 320 {
		t.Fatalf("editable scene lost: %v", err)
	}
}
