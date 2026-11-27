package editor

import (
	"strconv"

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
	current := strconv.FormatFloat(*n.Params.LoRAStrength, 'f', -1, 64)
	values := []string{"0", "0.25", "0.35", "0.5", "0.7", "0.8", "0.85", "1", "1.3", "1.5", "2"}
	labels := []string{i18n.T("LoRA 关闭"), "0.25", "0.35", "0.5", "0.7", "0.8", "0.85", "1", "1.3", "1.5", "2"}
	if v, ok := aiInlineSelect(c, "lora-strength-"+id, i18n.T("LoRA 强度"), current, values, labels, disabled); ok {
		strength, _ := strconv.ParseFloat(v, 64)
		b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Params.LoRAStrength = &strength })
	}
}
