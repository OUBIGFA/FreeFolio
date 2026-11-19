package app

import (
	"github.com/OUBIGFA/FreeMD/resources"
	"github.com/egoist/mygo/ui"
)

// Decode the shipped logo once, at a resolution that stays sharp on HiDPI.
var brandLogo = func() *ui.Bitmap {
	b, err := ui.DecodeBitmapScaled(resources.Icon, 128)
	if err != nil {
		panic(err)
	}
	return b
}()

func brand(c *ui.Context, context string, compact bool) {
	size := float32(32)
	if compact {
		size = 24
	}
	ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
		ui.Image(c, brandLogo).Size(size, size).Shrink(0)
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, "FreeMD").FontSize(16).FontWeight(650).NoWrap()
			if context != "" {
				ui.Text(c, context).FontSize(12).TextColor(c.Theme().TextMuted).NoWrap()
			}
		})
	})
}

// quietButton is MyGo's button as a toolbar shows it: no face of its own
// until hovered. A toolbar of one keeps the native look wherever it sits.
func quietButton(c *ui.Context, key string, content func(b ui.Element) ui.Element) ui.Element {
	var b ui.Element
	ui.Toolbar(c.Key(key+"-bar"), func() { b = content(ui.Button(c.Key(key), "")) }).Padding(0).Gap(0).Shrink(0)
	return b
}

// shellAction is a quiet button showing an icon before its label.
func shellAction(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	return quietButton(c, key, func(b ui.Element) ui.Element {
		return b.Label(label).Justify(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(15)
			ui.Text(c, label).SingleLine()
		})
	})
}

// segmentedChoice is MyGo's segmented control.
func segmentedChoice(c *ui.Context, selected *int, labels ...string) ui.Element {
	return ui.Segmented(c, selected, labels...)
}
