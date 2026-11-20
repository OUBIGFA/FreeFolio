package rough

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// Expected paths were produced by rough.js 4.6.4, not by this implementation.
func TestRoughMatchesExternalFixtures(t *testing.T) {
	b, err := os.ReadFile("../testdata/rough.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name    string
			Call    string
			Args    []json.RawMessage
			Options json.RawMessage
			Sets    []struct {
				Type string
				Ops  [][]json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) < 80 {
		t.Fatal("incomplete reference fixture")
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			o := DefaultOptions()
			if err := json.Unmarshal(c.Options, &o); err != nil {
				t.Fatal(err)
			}
			n := func(i int) float64 {
				var x float64
				if err := json.Unmarshal(c.Args[i], &x); err != nil {
					t.Fatal(err)
				}
				return x
			}
			var points []Point
			var text string
			var d Drawable
			switch c.Call {
			case "line":
				d = Line(n(0), n(1), n(2), n(3), o)
			case "rectangle":
				d = Rectangle(n(0), n(1), n(2), n(3), o)
			case "ellipse":
				d = Ellipse(n(0), n(1), n(2), n(3), o)
			case "circle":
				d = Ellipse(n(0), n(1), n(2), n(2), o)
			case "linearPath", "polygon", "curve":
				if err := json.Unmarshal(c.Args[0], &points); err != nil {
					t.Fatal(err)
				}
				switch c.Call {
				case "linearPath":
					d = LinearPath(points, o)
				case "polygon":
					d = Polygon(points, o)
				case "curve":
					d = Curve(points, o)
				}
			case "path":
				if err := json.Unmarshal(c.Args[0], &text); err != nil {
					t.Fatal(err)
				}
				d = Path(text, o)
			case "arc":
				var close bool
				_ = json.Unmarshal(c.Args[6], &close)
				d = Arc(n(0), n(1), n(2), n(3), n(4), n(5), close, o)
			default:
				t.Fatalf("unhandled fixture %s", c.Call)
			}
			if len(d.Sets) != len(c.Sets) {
				t.Fatalf("set count %d want %d", len(d.Sets), len(c.Sets))
			}
			for si, want := range c.Sets {
				got := d.Sets[si]
				if got.Type != want.Type || len(got.Ops) != len(want.Ops) {
					t.Fatalf("set %d: %s %d ops, want %s %d", si, got.Type, len(got.Ops), want.Type, len(want.Ops))
				}
				for i, w := range want.Ops {
					var kind string
					_ = json.Unmarshal(w[0], &kind)
					if got.Ops[i].Op != kind || len(got.Ops[i].Data) != len(w)-1 {
						t.Fatalf("set %d op %d mismatch: %+v", si, i, got.Ops[i])
					}
					for j, raw := range w[1:] {
						var v float64
						_ = json.Unmarshal(raw, &v)
						if math.Abs(got.Ops[i].Data[j]-v) > 1e-6 {
							t.Fatalf("set %d op %d value %d: %.12g want %.12g", si, i, j, got.Ops[i].Data[j], v)
						}
					}
				}
			}
		})
	}
}
