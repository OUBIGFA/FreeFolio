package mermaid

import (
	"slices"
)

// The layered layout places a flowchart the way dagre does for Mermaid:
// nodes go on ranks, with a middle rank between each pair of node ranks that
// carries edge labels; an edge spanning ranks becomes a chain of virtual
// nodes, so it routes around the nodes in its way instead of through them;
// barycenter sweeps order each rank while keeping a subgraph's members
// together; and order-preserving placement lines every node up with its
// neighbors so long edges run straight.

// lnode is a node of the layered graph: a flowchart node or a virtual node
// an edge passes through.
type lnode struct {
	real  int // flowNode index, or -1 for a virtual node
	edge  int // edge index of a virtual node
	label bool
	sg    int // innermost subgraph, -1 for none
	rank  int
	order int
	// cross and prim are the node's extent across the ranks and along the
	// rank direction; at is its center across the ranks.
	cross, prim float32
	at          float32
}

type layered struct {
	n        []lnode
	ranks    [][]int
	up, down [][]int
	// chains are each edge's nodes from its DAG source to its sink; nil for
	// a self loop.
	chains [][]int
	// center is each rank's center along the rank direction, thick its
	// thickest node's extent.
	center, thick []float32
}

const (
	layerNodeGap    float32 = 28
	layerVirtualGap float32 = 14
	layerClusterGap float32 = 18
	layerRankGap    float32 = 22
)

// layoutLayered ranks, orders and places nodes and edges. Sizes are already
// measured; horizontal swaps the axes for LR and RL charts.
func layoutLayered(nodes []*flowNode, edges []flowEdge, subgraphs []*flowSubgraph, horizontal bool, sgTop, sgBottom float32) *layered {
	g := &layered{}
	n := len(nodes)

	// Ranks by longest path over the DAG the reversed edges make.
	out := make([][]int, n)
	indeg := make([]int, n)
	for _, e := range edges {
		if e.from == e.to {
			continue
		}
		u, v := e.from, e.to
		if e.reversed {
			u, v = v, u
		}
		out[u] = append(out[u], v)
		indeg[v]++
	}
	rank := make([]int, n)
	var topo []int
	deg := slices.Clone(indeg)
	var queue []int
	for u := range n {
		if deg[u] == 0 {
			queue = append(queue, u)
		}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		topo = append(topo, u)
		for _, v := range out[u] {
			rank[v] = max(rank[v], rank[u]+1)
			if deg[v]--; deg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	// A source sits just above its nearest successor rather than on the top
	// rank, so it does not stretch its edges across the chart.
	for i := len(topo) - 1; i >= 0; i-- {
		u := topo[i]
		if indeg[u] != 0 || len(out[u]) == 0 {
			continue
		}
		lowest := -1
		for _, v := range out[u] {
			if lowest < 0 || rank[v] < lowest {
				lowest = rank[v]
			}
		}
		rank[u] = max(rank[u], lowest-1)
	}

	size := func(w, h float32) (cross, prim float32) {
		if horizontal {
			return h, w
		}
		return w, h
	}
	maxRank := 0
	for i, nd := range nodes {
		c, p := size(nd.w, nd.h)
		g.n = append(g.n, lnode{real: i, edge: -1, sg: nd.subgraph, rank: 2 * rank[i], cross: c, prim: p})
		maxRank = max(maxRank, 2*rank[i])
	}

	// Chains of virtual nodes; the label rides the one in the middle.
	g.chains = make([][]int, len(edges))
	for ei, e := range edges {
		if e.from == e.to {
			continue
		}
		u, v := e.from, e.to
		if e.reversed {
			u, v = v, u
		}
		sg := commonSubgraph(subgraphs, nodes[u].subgraph, nodes[v].subgraph)
		chain := []int{u}
		ru, rv := g.n[u].rank, g.n[v].rank
		mid := (ru + rv) / 2
		for r := ru + 1; r < rv; r++ {
			vn := lnode{real: -1, edge: ei, sg: sg, rank: r}
			if r == mid && e.label != "" {
				vn.label = true
				vn.cross, vn.prim = size(e.labelW, e.labelH)
			}
			chain = append(chain, len(g.n))
			g.n = append(g.n, vn)
		}
		chain = append(chain, v)
		g.chains[ei] = chain
	}

	g.up = make([][]int, len(g.n))
	g.down = make([][]int, len(g.n))
	for _, chain := range g.chains {
		for i := 1; i < len(chain); i++ {
			a, b := chain[i-1], chain[i]
			g.down[a] = append(g.down[a], b)
			g.up[b] = append(g.up[b], a)
		}
	}

	// Initial order: depth first from the nodes in the order they were
	// written, so a chart reads as its source does before any sweep.
	g.ranks = make([][]int, maxRank+1)
	seen := make([]bool, len(g.n))
	var visit func(int)
	visit = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		g.ranks[g.n[i].rank] = append(g.ranks[g.n[i].rank], i)
		for _, j := range g.down[i] {
			visit(j)
		}
	}
	for _, u := range topo {
		if indeg[u] == 0 {
			visit(u)
		}
	}
	for i := range g.n {
		visit(i)
	}
	g.renumber()

	g.order(subgraphs)
	g.place(subgraphs)

	// Positions along the rank direction: each rank as thick as its
	// thickest node, with room for subgraph titles and bottoms.
	thick := make([]float32, maxRank+1)
	before := make([]float32, maxRank+1)
	after := make([]float32, maxRank+1)
	for _, ln := range g.n {
		thick[ln.rank] = max(thick[ln.rank], ln.prim)
	}
	for si := range subgraphs {
		lo, hi := -1, -1
		for i, nd := range nodes {
			if !inSubgraph(subgraphs, nd.subgraph, si) {
				continue
			}
			r := g.n[i].rank
			if lo < 0 || r < lo {
				lo = r
			}
			hi = max(hi, r)
		}
		if lo >= 0 {
			before[lo] += sgTop
			after[hi] += sgBottom
		}
	}
	g.thick = thick
	g.center = make([]float32, maxRank+1)
	pos := float32(0)
	for r := 0; r <= maxRank; r++ {
		if r > 0 {
			pos += layerRankGap
		}
		pos += before[r]
		g.center[r] = pos + thick[r]/2
		pos += thick[r] + after[r]
	}
	return g
}

// commonSubgraph is the innermost subgraph holding both a and b, -1 for none.
func commonSubgraph(subgraphs []*flowSubgraph, a, b int) int {
	for s := a; s >= 0; s = subgraphs[s].parent {
		if inSubgraph(subgraphs, b, s) {
			return s
		}
	}
	return -1
}

// inSubgraph reports whether subgraph s is target or nested in it.
func inSubgraph(subgraphs []*flowSubgraph, s, target int) bool {
	for ; s >= 0; s = subgraphs[s].parent {
		if s == target {
			return true
		}
	}
	return false
}

// sgPath is the subgraphs holding s, outermost first.
func sgPath(subgraphs []*flowSubgraph, s int) []int {
	var path []int
	for ; s >= 0; s = subgraphs[s].parent {
		path = append(path, s)
	}
	slices.Reverse(path)
	return path
}

func (g *layered) renumber() {
	for _, row := range g.ranks {
		for o, i := range row {
			g.n[i].order = o
		}
	}
}

// order runs barycenter sweeps down and up the ranks and keeps the order
// with the fewest crossings.
func (g *layered) order(subgraphs []*flowSubgraph) {
	best := g.snapshot()
	bestCross := g.crossings()
	for sweep := 0; sweep < 24 && bestCross > 0; sweep++ {
		if sweep%2 == 0 {
			for r := 1; r < len(g.ranks); r++ {
				g.sortRank(r, g.up, subgraphs)
			}
		} else {
			for r := len(g.ranks) - 2; r >= 0; r-- {
				g.sortRank(r, g.down, subgraphs)
			}
		}
		if c := g.crossings(); c < bestCross {
			bestCross, best = c, g.snapshot()
		}
	}
	g.ranks = best
	g.renumber()
}

func (g *layered) snapshot() [][]int {
	out := make([][]int, len(g.ranks))
	for i, row := range g.ranks {
		out[i] = slices.Clone(row)
	}
	return out
}

// sortRank orders rank r by its neighbors' mean position in the adjacent
// rank, grouping the members of each subgraph together.
func (g *layered) sortRank(r int, nbrs [][]int, subgraphs []*flowSubgraph) {
	row := g.ranks[r]
	bary := make(map[int]float64, len(row))
	for _, i := range row {
		ns := nbrs[i]
		if len(ns) == 0 {
			bary[i] = float64(g.n[i].order)
			continue
		}
		sum := 0.0
		for _, j := range ns {
			sum += float64(g.n[j].order)
		}
		bary[i] = sum / float64(len(ns))
	}
	g.ranks[r] = groupedOrder(row, 0, bary, func(i int) []int { return sgPath(subgraphs, g.n[i].sg) }, func(i int) int { return g.n[i].order })
	for o, i := range g.ranks[r] {
		g.n[i].order = o
	}
}

// groupedOrder sorts items by bary while keeping those that share the
// subgraph at path depth together, recursively for nested subgraphs.
func groupedOrder(items []int, depth int, bary map[int]float64, path func(int) []int, prev func(int) int) []int {
	type group struct {
		key   int // subgraph at depth, or -1-item for an item outside one
		items []int
		bary  float64
		first int
	}
	var groups []*group
	byKey := map[int]*group{}
	for _, it := range items {
		p := path(it)
		key := -1 - it
		if depth < len(p) {
			key = p[depth]
		}
		gr := byKey[key]
		if gr == nil {
			gr = &group{key: key, first: prev(it)}
			byKey[key] = gr
			groups = append(groups, gr)
		}
		gr.items = append(gr.items, it)
		gr.bary += bary[it]
		gr.first = min(gr.first, prev(it))
	}
	for _, gr := range groups {
		gr.bary /= float64(len(gr.items))
	}
	slices.SortStableFunc(groups, func(a, b *group) int {
		switch {
		case a.bary < b.bary:
			return -1
		case a.bary > b.bary:
			return 1
		}
		return a.first - b.first
	})
	var out []int
	for _, gr := range groups {
		if gr.key >= 0 && len(gr.items) > 1 {
			out = append(out, groupedOrder(gr.items, depth+1, bary, path, prev)...)
		} else {
			out = append(out, gr.items...)
		}
	}
	return out
}

// crossings counts the edge crossings between adjacent ranks.
func (g *layered) crossings() int {
	total := 0
	for r := 0; r+1 < len(g.ranks); r++ {
		var segs [][2]int
		for _, i := range g.ranks[r] {
			for _, j := range g.down[i] {
				segs = append(segs, [2]int{g.n[i].order, g.n[j].order})
			}
		}
		for a := 0; a < len(segs); a++ {
			for b := a + 1; b < len(segs); b++ {
				if (segs[a][0]-segs[b][0])*(segs[a][1]-segs[b][1]) < 0 {
					total++
				}
			}
		}
	}
	return total
}

// gap is the space kept between neighbors a and b on a rank: wider between
// real nodes, and wider still across a subgraph's edge.
func (g *layered) gap(subgraphs []*flowSubgraph, a, b int) float32 {
	na, nb := g.n[a], g.n[b]
	gap := layerNodeGap
	if na.real < 0 || nb.real < 0 {
		gap = layerVirtualGap
	}
	pa, pb := sgPath(subgraphs, na.sg), sgPath(subgraphs, nb.sg)
	common := 0
	for common < len(pa) && common < len(pb) && pa[common] == pb[common] {
		common++
	}
	return gap + layerClusterGap*float32(len(pa)-common+len(pb)-common)
}

// place sets each node's center across the ranks. Sweeps pull every node
// toward its neighbors' weighted mean, and each rank is then placed as close
// to those targets as order and spacing allow (isotonic regression).
// Edges between virtual nodes weigh most, so long edges run straight.
func (g *layered) place(subgraphs []*flowSubgraph) {
	seps := make([][]float32, len(g.ranks))
	for r, row := range g.ranks {
		seps[r] = make([]float32, len(row))
		x := float32(0)
		for k, i := range row {
			if k > 0 {
				prev := row[k-1]
				seps[r][k] = (g.n[prev].cross+g.n[i].cross)/2 + g.gap(subgraphs, prev, i)
				x += seps[r][k]
			}
			g.n[i].at = x
		}
	}
	weight := func(a, b int) float64 {
		va, vb := g.n[a].real < 0, g.n[b].real < 0
		switch {
		case va && vb:
			return 8
		case va || vb:
			return 2
		}
		return 1
	}
	pull := func(r int, sides ...[][]int) {
		row := g.ranks[r]
		want := make([]float64, len(row))
		wts := make([]float64, len(row))
		for k, i := range row {
			sum, w := 0.0, 0.0
			for _, side := range sides {
				for _, j := range side[i] {
					wt := weight(i, j)
					sum += float64(g.n[j].at) * wt
					w += wt
				}
			}
			if w == 0 {
				want[k], wts[k] = float64(g.n[i].at), 0.01
				continue
			}
			want[k], wts[k] = sum/w, w
		}
		for k, x := range isotonic(want, wts, seps[r]) {
			g.n[row[k]].at = float32(x)
		}
	}
	for iter := 0; iter < 16; iter++ {
		if iter%2 == 0 {
			for r := 1; r < len(g.ranks); r++ {
				pull(r, g.up)
			}
		} else {
			for r := len(g.ranks) - 2; r >= 0; r-- {
				pull(r, g.down)
			}
		}
	}
	for r := range g.ranks {
		pull(r, g.up, g.down)
	}
	// Start at zero: the leftmost edge of anything on any rank.
	lo := float32(0)
	first := true
	for _, ln := range g.n {
		if left := ln.at - ln.cross/2; first || left < lo {
			lo, first = left, false
		}
	}
	for i := range g.n {
		g.n[i].at -= lo
	}
}

// isotonic places a rank: x[k] as close to want[k] (weighted) as possible
// while x[k]-x[k-1] >= sep[k]. With offsets o, x[k] = o[k]+t[k] for a
// nondecreasing t, which pool-adjacent-violators solves exactly.
func isotonic(want, wts []float64, sep []float32) []float64 {
	n := len(want)
	off := make([]float64, n)
	for k := 1; k < n; k++ {
		off[k] = off[k-1] + float64(sep[k])
	}
	type block struct {
		sum, w float64
		from   int
	}
	var blocks []block
	for k := range n {
		blocks = append(blocks, block{sum: (want[k] - off[k]) * wts[k], w: wts[k], from: k})
		for len(blocks) > 1 {
			a, b := blocks[len(blocks)-2], blocks[len(blocks)-1]
			if a.sum/a.w <= b.sum/b.w {
				break
			}
			blocks = append(blocks[:len(blocks)-2], block{sum: a.sum + b.sum, w: a.w + b.w, from: a.from})
		}
	}
	x := make([]float64, n)
	for bi, b := range blocks {
		end := n
		if bi+1 < len(blocks) {
			end = blocks[bi+1].from
		}
		t := b.sum / b.w
		for k := b.from; k < end; k++ {
			x[k] = off[k] + t
		}
	}
	return x
}

// routeChain draws an edge through its chain: out of each node along the
// rank direction, across halfway between two ranks, and into the next. It
// also gives the center of the label's virtual node, when there is one.
func (g *layered) routeChain(chain []int, prim func(float32) float32, crossPad float32, horizontal, _ bool) (pts [][2]float32, label [2]float32, hasLabel bool) {
	add := func(cross, p float32) {
		pt := [2]float32{crossPad + cross, prim(p)}
		if horizontal {
			pt = [2]float32{pt[1], pt[0]}
		}
		if k := len(pts); k > 0 && pts[k-1] == pt {
			return
		}
		// Drop the middle of three points on one line.
		if k := len(pts); k > 1 {
			a, b := pts[k-2], pts[k-1]
			if (a[0] == b[0] && b[0] == pt[0]) || (a[1] == b[1] && b[1] == pt[1]) {
				pts[k-1] = pt
				return
			}
		}
		pts = append(pts, pt)
	}
	for k := 0; k+1 < len(chain); k++ {
		a, b := g.n[chain[k]], g.n[chain[k+1]]
		exit := g.center[a.rank] + a.prim/2
		entry := g.center[b.rank] - b.prim/2
		if k == 0 {
			add(a.at, exit)
		}
		if a.at != b.at {
			// Bend in the gap between the two ranks, clear of every node
			// on either.
			mid := (g.center[a.rank] + g.thick[a.rank]/2 + g.center[b.rank] - g.thick[b.rank]/2) / 2
			add(a.at, mid)
			add(b.at, mid)
		}
		add(b.at, entry)
		if b.label {
			label = [2]float32{crossPad + b.at, prim(g.center[b.rank])}
			if horizontal {
				label = [2]float32{label[1], label[0]}
			}
			hasLabel = true
		}
		if b.real < 0 {
			add(b.at, g.center[b.rank]+b.prim/2)
		}
	}
	return pts, label, hasLabel
}
