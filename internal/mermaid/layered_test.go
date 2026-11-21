package mermaid

import "testing"

// A labeled fan-out, a long edge past a node, a back edge and subgraphs lay
// out without nodes overlapping, labels on nodes, or edges through nodes.
func TestLayeredFlowchartKeepsEdgesAndLabelsOffNodes(t *testing.T) {
	for _, src := range []string{
		`graph TD
    A[用户请求] --> B[语义解析]
    B --> C[RAG检索]
    C -->|✅ 知识库匹配| D[上下文增强]
    C -->|❌ 无匹配| E[任务分解]
    D --> E
    E --> F{工具选择}
    F -->|🛠️ 核心工具| G{基础操作}
    F -->|🔌 MCP扩展服务| H{MCP操作}
    G -->|✏️ 文件操作| I[读写/替换]
    G -->|🖥️ 系统命令执行| J[执行命令]
    G -->|🔍 代码分析| K[代码分析]
    H -->|⚙️ 使用MCP工具| L[使用MCP工具]
    H -->|📦 访问MCP资源| M[访问MCP资源]
    I --> N[结果验证]
    J --> N
    K --> N
    L --> N
    M --> N
    N --> O{完成判断}
    O -->|✅| P[提交最终结果]
    O -->|❌| E`,
		`flowchart LR
  subgraph A1 [订单流转域]
    A[用户下单] --> B{库存检查}
    B -- 库存充足 --> C[预占库存]
    B -- 库存不足 --> D[下发缺货通知]
  end
  subgraph B1 [履约域]
    C --> H[拣货打包]
  end
  A --> H`,
	} {
		d, err := ParseAndLayout(src, nil)
		if err != nil {
			t.Fatal(err)
		}
		assertDiagramBounds(t, d)
		var nodes []ShapeOp
		for _, s := range d.Shapes {
			if s.Role == RoleNode {
				nodes = append(nodes, s)
			}
		}
		inside := func(s ShapeOp, x, y float32) bool {
			return x > s.X+1 && x < s.X+s.W-1 && y > s.Y+1 && y < s.Y+s.H-1
		}
		for i, a := range nodes {
			for _, b := range nodes[i+1:] {
				if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
					t.Fatalf("nodes overlap: %+v %+v", a, b)
				}
			}
		}
		for _, lb := range d.Labels {
			if lb.Role != RoleEdgeLabel {
				continue
			}
			for _, n := range nodes {
				if lb.X < n.X+n.W && n.X < lb.X+lb.W && lb.Y < n.Y+n.H && n.Y < lb.Y+lb.H {
					t.Fatalf("edge label %q covers a node %+v", lb.Text, n)
				}
			}
		}
		for _, p := range d.Paths {
			for k := 0; k+1 < len(p.Points); k++ {
				a, b := p.Points[k], p.Points[k+1]
				for step := float32(0.1); step < 1; step += 0.1 {
					x, y := a[0]+(b[0]-a[0])*step, a[1]+(b[1]-a[1])*step
					for _, n := range nodes {
						if inside(n, x, y) {
							t.Fatalf("edge %v passes through node %+v", p.Points, n)
						}
					}
				}
			}
		}
	}
}
