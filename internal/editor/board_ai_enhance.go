package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// aiLoRAStrength stores an explicit zero and keeps it distinct from defaults.
func (b *Board) aiLoRAStrength(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	if n.Params.LoRAStrength == nil {
		return
	}
	ui.Text(c, i18n.T("LoRA 强度")).FontSize(11.5).TextColor(o.Theme.TextMuted).Shrink(0)
	strength := *n.Params.LoRAStrength
	value := strength
	ui.NumberInput(c.Key("lora-strength-"+id), &value, -100, 100, .01).Grow(1).MinWidth(0).Height(26).Disabled(disabled).Label(i18n.T("LoRA 强度"))
	if value != strength && !disabled {
		b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Params.LoRAStrength = &value })
	}
}
