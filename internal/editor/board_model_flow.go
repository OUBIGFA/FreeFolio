package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// insertAIModelPreset creates real, editable model links in one undo step.
func (b *Board) insertAIModelPreset(preset BoardAIPreset, label string, o BoardOptions) {
	if b.ReadOnly {
		return
	}
	m := preset.Model
	b.edit(o, func() {
		at := b.viewCenter()
		genAt := at
		if m.BaseModel != "" {
			genAt[0] += 400
		}
		gen := b.newAINode(preset.Role, genAt, o)
		n := whiteboard.AINodeOf(gen)
		n.Title = label
		n.Connection, n.Model = m.Connection, m.Model
		n.Instruction = m.Instruction
		n.Params.LoRAStrength = m.LoRAStrength
		if m.BaseModel != "" {
			base := b.newAINode(whiteboard.AIModel, whiteboard.Point{at[0] - 400, at[1]}, o)
			bn := whiteboard.AINodeOf(base)
			bn.Connection, bn.Model = m.Connection, m.BaseModel
			bn.Title = i18n.T("加载底模")
			_ = whiteboard.SetAINode(base, bn)
			lora := b.newAINode(whiteboard.AILoRA, at, o)
			ln := whiteboard.AINodeOf(lora)
			ln.Title = m.Name
			ln.Connection, ln.Model = m.Connection, m.Model
			ln.Params.LoRAStrength = m.LoRAStrength
			ln.Connect("model", base.ID)
			_ = whiteboard.SetAINode(lora, ln)
			n.Model = m.BaseModel
			n.Params.LoRAStrength = nil
			n.Connect("model", lora.ID)
		}
		if preset.Role == whiteboard.AIImage {
			n.Params.Size = m.defaultImageSize()
			n.Params.Quality = ""
		}
		gen.H = max(gen.H, aiMinHeight(n))
		b.err = whiteboard.SetAINode(gen, n)
		b.selectIDs([]string{gen.ID})
	})
	if m.BaseModel != "" {
		b.ai.bar = false
		b.fit(false)
	}
}
