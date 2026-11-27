package whiteboard

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The node graph follows LiteGraph, the graph core of ComfyUI
// (Comfy-Org/litegraph.js, MIT, revision ea0e1a85): every node has typed
// input slots and one typed output; an input holds at most one link and a
// new link replaces it, while an output feeds any number of inputs. A link
// dropped on a node goes to its first free slot of the link's type
// (findFreeSlotOfType), and types match as isValidConnection decides. An
// input slot that backs a widget (ComfyUI's widget input) disables that
// widget while it is linked. The functions below are Go ports of those
// rules; links are kept in the target node's customData, apart from the
// board's own lines, so a data line is never an Excalidraw arrow.

// Slot types, as LiteGraph names them. "*" matches any type.
const (
	AITypeString = "STRING"
	AITypeImage  = "IMAGE"
	AITypeVideo  = "VIDEO"
)

// AIMaxListSlots bounds a growing input list, such as reference pictures.
const AIMaxListSlots = 8

// AISlot is one input slot of a node.
type AISlot struct {
	// Name is the slot's key in links: a group, or group.index in a list.
	Name, Group, Label, Type string
	// Widget marks the slot that feeds the node's instruction field; the
	// field is disabled while the slot is linked.
	Widget bool
	// Disabled slots remain visible, but cannot feed or schedule a node.
	Disabled bool
	// Number is a list slot's place from 1 while the list shows more than
	// one slot, else 0; it follows Label where the slot is shown.
	Number int
}

// AILink is a link into a node's input slot from an element: another
// node's output, or a board element read as text or a picture.
type AILink struct {
	Slot string `json:"slot"`
	From string `json:"from"`
}

type aiSlotSpec struct {
	group, label, typ string
	list, widget      bool
}

// aiSlotSpecs are the input slots of each role, in drawing order. Labels
// are interface text, translated where they are shown.
var aiSlotSpecs = map[AIRole][]aiSlotSpec{
	AICutout:  {{"image", "输入图片", AITypeImage, false, false}, {"prompt", "抠图说明", AITypeString, false, true}},
	AIUpscale: {{"image", "输入图片", AITypeImage, false, false}},
	AICompare: {{"a", "图片 A", AITypeImage, false, false}, {"b", "图片 B", AITypeImage, false, false}},
	AIText:    {{"prompt", "指令", AITypeString, false, true}, {"text", "文本", AITypeString, true, false}, {"image", "图片", AITypeImage, true, false}},
	AIImage:   {{"prompt", "提示词", AITypeString, false, true}, {"image", "参考图", AITypeImage, true, false}, {"mask", "遮罩", AITypeImage, false, false}},
	AIVideo:   {{"prompt", "提示词", AITypeString, false, true}, {"image", "首帧", AITypeImage, false, false}},
	AIConcat:  {{"text", "文本", AITypeString, true, false}},
}

// AcceptsPrompt follows the inputs consumed by the model adapters. In
// particular, automatic matting and super-resolution do not consume text.
func (n *AINode) AcceptsPrompt() bool {
	switch n.Role {
	case AICutout:
		return n.Connection == "comfyui-rmbg3" || n.Connection == "comfyui-sa2va"
	case AIPrompt, AIText, AIImage, AIVideo:
		return true
	}
	return false
}

// AIOutputType is the type of a role's output, or "" for a node whose
// result is not passed on.
func AIOutputType(role AIRole) string {
	switch role {
	case AIImage, AIImageInput, AICutout, AIUpscale:
		return AITypeImage
	case AIVideo, AICompare:
		return ""
	}
	return AITypeString
}

// listIndex splits a list slot name; ok is false for a single slot.
func listIndex(name string) (string, int, bool) {
	group, idx, found := strings.Cut(name, ".")
	if !found {
		return name, 0, false
	}
	i, err := strconv.Atoi(idx)
	return group, i, err == nil
}

// AISlots are a node's input slots. A list shows one free slot after its
// linked ones, as ComfyUI's growing inputs do, up to AIMaxListSlots.
func AISlots(n *AINode) []AISlot {
	var out []AISlot
	for _, sp := range aiSlotSpecs[n.Role] {
		if !sp.list {
			out = append(out, AISlot{Name: sp.group, Group: sp.group, Label: sp.label, Type: sp.typ, Widget: sp.widget, Disabled: sp.widget && !n.AcceptsPrompt()})
			continue
		}
		count := 0
		for _, l := range n.Links {
			if g, i, ok := listIndex(l.Slot); ok && g == sp.group {
				count = max(count, i+1)
			}
		}
		count = min(AIMaxListSlots, count+1)
		for i := range count {
			number := 0
			if count > 1 {
				number = i + 1
			}
			out = append(out, AISlot{Name: fmt.Sprintf("%s.%d", sp.group, i), Group: sp.group, Label: sp.label, Type: sp.typ, Number: number})
		}
	}
	return out
}

// AISlotByName is a node's slot by its name.
func AISlotByName(n *AINode, name string) (AISlot, bool) {
	for _, sl := range AISlots(n) {
		if sl.Name == name {
			return sl, true
		}
	}
	return AISlot{}, false
}

// LinkAt is the element linked into a slot, or "".
func (n *AINode) LinkAt(slot string) string {
	for _, l := range n.Links {
		if l.Slot == slot {
			return l.From
		}
	}
	return ""
}

// Connect links an element into a slot, replacing the slot's link as
// LiteGraph's connectSlots does.
func (n *AINode) Connect(slot, from string) {
	if sl, ok := AISlotByName(n, slot); ok && sl.Disabled {
		return
	}
	n.Links = slices.DeleteFunc(slices.Clone(n.Links), func(l AILink) bool { return l.Slot == slot })
	n.Links = append(n.Links, AILink{Slot: slot, From: from})
}

// Disconnect removes a slot's link. A list closes the gap, so its linked
// slots stay first and one free slot follows them.
func (n *AINode) Disconnect(slot string) {
	n.Links = slices.DeleteFunc(slices.Clone(n.Links), func(l AILink) bool { return l.Slot == slot })
	group, at, ok := listIndex(slot)
	if !ok {
		return
	}
	for i, l := range n.Links {
		if g, k, ok := listIndex(l.Slot); ok && g == group && k > at {
			n.Links[i].Slot = fmt.Sprintf("%s.%d", g, k-1)
		}
	}
}

// AIValidConnection reports whether an output of type a may feed an input
// of type b: LiteGraph's isValidConnection, where "" and "*" match any
// type, names match case-insensitively, and a comma lists several types.
func AIValidConnection(a, b string) bool {
	if a == "" || a == "*" || b == "" || b == "*" || a == b {
		return true
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	if !strings.Contains(a, ",") && !strings.Contains(b, ",") {
		return a == b
	}
	for _, x := range strings.Split(a, ",") {
		for _, y := range strings.Split(b, ",") {
			if AIValidConnection(x, y) {
				return true
			}
		}
	}
	return false
}

// AIFindInputByType is the slot a link of the type dropped on the node
// goes to: LiteGraph's findFreeSlotOfType. The first free slot of the type
// wins; then the first free wildcard slot, the first occupied slot of the
// type, and the first occupied wildcard slot.
func AIFindInputByType(n *AINode, typ string) (AISlot, bool) {
	var occupied, wild, occupiedWild *AISlot
	want := strings.Split(strings.ToLower(typ), ",")
	slots := AISlots(n)
	for i := range slots {
		sl := &slots[i]
		if sl.Disabled {
			continue
		}
		free := n.LinkAt(sl.Name) == ""
		for _, w := range want {
			for _, have := range strings.Split(strings.ToLower(sl.Type), ",") {
				switch {
				case have == w:
					if free {
						return *sl, true
					}
					if occupied == nil {
						occupied = sl
					}
				case wild == nil && (w == "*" || have == "*"):
					if free {
						wild = sl
					} else if occupiedWild == nil {
						occupiedWild = sl
					}
				}
			}
		}
	}
	for _, sl := range []*AISlot{wild, occupied, occupiedWild} {
		if sl != nil {
			return *sl, true
		}
	}
	return AISlot{}, false
}

// AISourceType is the type an element supplies to a link: a node's output
// type, a picture for images and frames, text for anything that shows text
// or names a [[document]]. Lines and strokes supply nothing ("").
func (s *Scene) AISourceType(e *Element) string {
	if e == nil || e.IsDeleted {
		return ""
	}
	if n := AINodeOf(e); n != nil {
		return AIOutputType(n.Role)
	}
	switch {
	case e.IsLinear() || e.Kind == KindFreedraw || e.Kind == KindText && e.ContainerID != "":
		return ""
	case e.Kind == KindImage || e.IsFrame():
		return AITypeImage
	}
	return AITypeString
}

// AILinkSource is the live element linked into a slot, or nil when the
// link names an element that is gone or the selected model disables it.
func (s *Scene) AILinkSource(n *AINode, slot string) *Element {
	// Keep stored links intact so switching back restores the user's graph.
	if slot == "prompt" && !n.AcceptsPrompt() {
		return nil
	}
	e := s.ElementByID(n.LinkAt(slot))
	if e == nil || e.IsDeleted {
		return nil
	}
	return e
}

// AICreatesCycle reports whether linking from into to would make a node
// read its own result: to is from, or from already reads to.
func (s *Scene) AICreatesCycle(from, to string) bool {
	if from == to {
		return true
	}
	deps := s.AIDependencies()
	seen := map[string]bool{}
	var reaches func(string) bool
	reaches = func(id string) bool {
		if id == to {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		return slices.ContainsFunc(deps[id], reaches)
	}
	return reaches(from)
}

// remapAILinks points a pasted node's links at the pasted copies of their
// sources and drops links to elements that were not copied, as a plain
// paste in ComfyUI keeps only the links inside the copied set.
func remapAILinks(e *Element, ids map[string]string) {
	n := AINodeOf(e)
	if n == nil || len(n.Links) == 0 {
		return
	}
	var links []AILink
	for _, l := range n.Links {
		if id := ids[l.From]; id != "" {
			links = append(links, AILink{Slot: l.Slot, From: id})
		}
	}
	n.Links = compactAILinks(links)
	_ = SetAINode(e, n)
}

// compactAILinks renumbers each list's links from zero, keeping their
// order within the list.
func compactAILinks(links []AILink) []AILink {
	order := slices.Clone(links)
	slices.SortStableFunc(order, func(a, b AILink) int {
		_, ia, _ := listIndex(a.Slot)
		_, ib, _ := listIndex(b.Slot)
		return ia - ib
	})
	next := map[string]int{}
	out := make([]AILink, 0, len(order))
	for _, l := range order {
		if g, _, ok := listIndex(l.Slot); ok {
			l.Slot = fmt.Sprintf("%s.%d", g, next[g])
			next[g]++
		}
		out = append(out, l)
	}
	return out
}
