// TrueType subsetting adapts Quiet.md e59c49a board/font_subset.rs (MIT).
// Composite closure is retained. Latin layout tables keep stable glyph IDs;
// CJK exports renumber glyphs and omit layout tables, as the source does.
package whiteboard

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
)

func fontU16(d []byte, o int) (uint16, bool) {
	if o < 0 || o+2 > len(d) {
		return 0, false
	}
	return binary.BigEndian.Uint16(d[o:]), true
}
func fontU32(d []byte, o int) (uint32, bool) {
	if o < 0 || o+4 > len(d) {
		return 0, false
	}
	return binary.BigEndian.Uint32(d[o:]), true
}
func fontTables(font []byte) (map[string][]byte, error) {
	n, ok := fontU16(font, 4)
	if !ok {
		return nil, fmt.Errorf("invalid font header")
	}
	out := map[string][]byte{}
	for i := 0; i < int(n); i++ {
		r := 12 + i*16
		off, a := fontU32(font, r+8)
		size, b := fontU32(font, r+12)
		if !a || !b || uint64(off)+uint64(size) > uint64(len(font)) {
			return nil, fmt.Errorf("invalid font table")
		}
		out[string(font[r:r+4])] = font[off : off+size]
	}
	return out, nil
}
func fontMapping(cmap []byte) map[uint32]uint16 {
	out := map[uint32]uint16{}
	n, ok := fontU16(cmap, 2)
	if !ok {
		return out
	}
	best, rank := -1, 0
	for i := 0; i < int(n); i++ {
		r := 4 + i*8
		p, a := fontU16(cmap, r)
		enc, b := fontU16(cmap, r+2)
		off, c := fontU32(cmap, r+4)
		format, d := fontU16(cmap, int(off))
		if !a || !b || !c || !d {
			continue
		}
		score := 0
		if format == 12 && ((p == 3 && enc == 10) || p == 0) {
			score = 3
		} else if format == 4 && ((p == 3 && enc == 1) || p == 0) {
			score = 2
		}
		if score > rank {
			best, rank = int(off), score
		}
	}
	if best < 0 {
		return out
	}
	t := cmap[best:]
	format, _ := fontU16(t, 0)
	if format == 12 {
		groups, _ := fontU32(t, 12)
		for i := 0; i < int(groups) && 16+i*12+12 <= len(t); i++ {
			r := 16 + i*12
			s, _ := fontU32(t, r)
			e, _ := fontU32(t, r+4)
			g, _ := fontU32(t, r+8)
			if e > 0x10ffff {
				continue
			}
			for cp := s; cp <= e; cp++ {
				gid := g + cp - s
				if gid > 0 && gid <= 65535 {
					out[cp] = uint16(gid)
				}
			}
		}
	} else {
		segCount, _ := fontU16(t, 6)
		count := int(segCount) / 2
		for i := 0; i < count; i++ {
			end, a := fontU16(t, 14+i*2)
			start, b := fontU16(t, 16+count*2+i*2)
			delta, c := fontU16(t, 16+count*4+i*2)
			at := 16 + count*6 + i*2
			ro, d := fontU16(t, at)
			if !a || !b || !c || !d {
				continue
			}
			for cp := uint32(start); cp <= uint32(end) && cp < 65535; cp++ {
				g := uint16(cp) + delta
				if ro != 0 {
					v, ok := fontU16(t, at+int(ro)+int(cp-uint32(start))*2)
					if !ok || v == 0 {
						continue
					}
					g = v + delta
				}
				if g != 0 {
					out[cp] = g
				}
			}
		}
	}
	return out
}
func fontComponents(g []byte) [][2]int {
	var out [][2]int
	if len(g) < 10 {
		return out
	}
	n, _ := fontU16(g, 0)
	if int16(n) >= 0 {
		return out
	}
	for o := 10; o+4 <= len(g); {
		flags, _ := fontU16(g, o)
		id, _ := fontU16(g, o+2)
		out = append(out, [2]int{o + 2, int(id)})
		o += 4
		if flags&1 != 0 {
			o += 4
		} else {
			o += 2
		}
		if flags&8 != 0 {
			o += 2
		} else if flags&0x40 != 0 {
			o += 4
		} else if flags&0x80 != 0 {
			o += 8
		}
		if flags&0x20 == 0 {
			break
		}
	}
	return out
}
func fontChecksum(data []byte) uint32 {
	sum := uint32(0)
	for len(data) > 0 {
		var p [4]byte
		n := copy(p[:], data)
		sum += binary.BigEndian.Uint32(p[:])
		data = data[n:]
	}
	return sum
}
func fontCMap(mapping map[uint32]uint16) []byte {
	var codes []uint32
	for c := range mapping {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	table := []byte{0, 0, 0, 1, 0, 3, 0, 10, 0, 0, 0, 12}
	table = binary.BigEndian.AppendUint16(table, 12)
	table = binary.BigEndian.AppendUint16(table, 0)
	table = binary.BigEndian.AppendUint32(table, uint32(16+len(codes)*12))
	table = binary.BigEndian.AppendUint32(table, 0)
	table = binary.BigEndian.AppendUint32(table, uint32(len(codes)))
	for _, c := range codes {
		table = binary.BigEndian.AppendUint32(table, c)
		table = binary.BigEndian.AppendUint32(table, c)
		table = binary.BigEndian.AppendUint32(table, uint32(mapping[c]))
	}
	return table
}
func SubsetFont(font []byte, chars []rune, keepLayout bool) ([]byte, error) {
	tabs, err := fontTables(font)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"head", "maxp", "hhea", "hmtx", "loca", "glyf", "cmap"} {
		if len(tabs[name]) == 0 {
			return nil, fmt.Errorf("font lacks %s", name)
		}
	}
	head, maxp, hhea, hmtx, loca, glyf := tabs["head"], tabs["maxp"], tabs["hhea"], tabs["hmtx"], tabs["loca"], tabs["glyf"]
	num, ok := fontU16(maxp, 4)
	if !ok || len(head) < 54 || len(hhea) < 36 {
		return nil, fmt.Errorf("invalid font metrics")
	}
	long := binary.BigEndian.Uint16(head[50:]) == 1
	at := func(i int) (int, bool) {
		if long {
			v, ok := fontU32(loca, i*4)
			return int(v), ok
		}
		v, ok := fontU16(loca, i*2)
		return int(v) * 2, ok
	}
	glyph := func(i int) []byte {
		a, ok := at(i)
		b, ok2 := at(i + 1)
		if !ok || !ok2 || a > b || b > len(glyf) {
			return nil
		}
		return glyf[a:b]
	}
	hm := int(binary.BigEndian.Uint16(hhea[34:]))
	if hm == 0 {
		return nil, fmt.Errorf("invalid font advances")
	}
	mapping := fontMapping(tabs["cmap"])
	selected := map[uint32]uint16{}
	keep := map[uint16]bool{0: true}
	for _, c := range chars {
		if id := mapping[uint32(c)]; id > 0 {
			selected[uint32(c)] = id
			keep[id] = true
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	if keepLayout {
		encoded := map[uint16]bool{}
		for _, g := range mapping {
			encoded[g] = true
		}
		for i := 0; i < int(num); i++ {
			if !encoded[uint16(i)] {
				keep[uint16(i)] = true
			}
		}
	}
	stack := make([]uint16, 0, len(keep))
	for g := range keep {
		stack = append(stack, g)
	}
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range fontComponents(glyph(int(g))) {
			id := uint16(c[1])
			if id < num && !keep[id] {
				keep[id] = true
				stack = append(stack, id)
			}
		}
	}
	var order []uint16
	if keepLayout {
		for i := 0; i < int(num); i++ {
			order = append(order, uint16(i))
		}
	} else {
		for g := range keep {
			order = append(order, g)
		}
		sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	}
	newID := map[uint16]uint16{}
	for i, g := range order {
		newID[g] = uint16(i)
	}
	var ng, nl, nh []byte
	for _, g := range order {
		nl = binary.BigEndian.AppendUint32(nl, uint32(len(ng)))
		i := int(g)
		adv, _ := fontU16(hmtx, (min(hm, i+1)-1)*4)
		lsbAt := i*4 + 2
		if i >= hm {
			lsbAt = hm*4 + (i-hm)*2
		}
		lsb, _ := fontU16(hmtx, lsbAt)
		nh = binary.BigEndian.AppendUint16(nh, adv)
		nh = binary.BigEndian.AppendUint16(nh, lsb)
		if !keep[g] {
			continue
		}
		data := append([]byte{}, glyph(i)...)
		if !keepLayout {
			for _, c := range fontComponents(data) {
				binary.BigEndian.PutUint16(data[c[0]:], newID[uint16(c[1])])
			}
		}
		ng = append(ng, data...)
		for len(ng)%4 != 0 {
			ng = append(ng, 0)
		}
	}
	nl = binary.BigEndian.AppendUint32(nl, uint32(len(ng)))
	outTabs := map[string][]byte{}
	drop := map[string]bool{"DSIG": true, "hdmx": true, "LTSH": true, "VDMX": true, "FFTM": true}
	if !keepLayout {
		for _, t := range []string{"GSUB", "GPOS", "GDEF", "kern", "vmtx", "vhea", "BASE", "JSTF", "MATH"} {
			drop[t] = true
		}
	}
	for tag, data := range tabs {
		if !drop[tag] {
			outTabs[tag] = append([]byte{}, data...)
		}
	}
	outTabs["glyf"], outTabs["loca"], outTabs["hmtx"] = ng, nl, nh
	binary.BigEndian.PutUint32(outTabs["head"][8:], 0)
	binary.BigEndian.PutUint16(outTabs["head"][50:], 1)
	binary.BigEndian.PutUint16(outTabs["maxp"][4:], uint16(len(order)))
	binary.BigEndian.PutUint16(outTabs["hhea"][34:], uint16(len(order)))
	for cp, id := range selected {
		selected[cp] = newID[id]
	}
	outTabs["cmap"] = fontCMap(selected)
	if len(outTabs["post"]) >= 32 {
		outTabs["post"] = outTabs["post"][:32]
		binary.BigEndian.PutUint32(outTabs["post"], 0x30000)
	}
	var tags []string
	for tag := range outTabs {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	n := len(tags)
	pow := 1 << (bits.Len(uint(n)) - 1)
	out := append([]byte{}, font[:4]...)
	for _, v := range []int{n, pow * 16, bits.Len(uint(pow)) - 1, n*16 - pow*16} {
		out = binary.BigEndian.AppendUint16(out, uint16(v))
	}
	off := 12 + n*16
	headAt := 0
	for _, tag := range tags {
		data := outTabs[tag]
		if tag == "head" {
			headAt = off
		}
		out = append(out, tag...)
		out = binary.BigEndian.AppendUint32(out, fontChecksum(data))
		out = binary.BigEndian.AppendUint32(out, uint32(off))
		out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
		off += (len(data) + 3) / 4 * 4
	}
	for _, tag := range tags {
		out = append(out, outTabs[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	binary.BigEndian.PutUint32(out[headAt+8:], 0xb1b0afba-fontChecksum(out))
	return out, nil
}
