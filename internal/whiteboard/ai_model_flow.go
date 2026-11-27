package whiteboard

import "fmt"

// AILoRAConfig is a model adapter in upstream-to-downstream order.
type AILoRAConfig struct {
	ID       string
	Strength *float64
}
type AIModelConfig struct {
	Connection, Model string
	LoRAs             []AILoRAConfig
}

// AIInputNode follows the upstream model's input capability without changing
// the node's saved selection, which remains available on disconnection.
func (s *Scene) AIInputNode(n *AINode) *AINode {
	if n != nil && n.ImageHistory() && n.LinkAt("model") != "" {
		if flow, err := s.AIModelFlow(n.LinkAt("model")); err == nil && flow != nil {
			copy := *n
			copy.Connection = flow.Connection
			return &copy
		}
	}
	return n
}

// AIModelFlow resolves configuration links even for a run of only the final
// node. No model weights or transient outputs are stored in the document.
func (s *Scene) AIModelFlow(id string) (*AIModelConfig, error) {
	seen := map[string]bool{}
	var visit func(string) (*AIModelConfig, error)
	visit = func(id string) (*AIModelConfig, error) {
		if seen[id] {
			return nil, ErrAICycle
		}
		seen[id] = true
		e := s.ElementByID(id)
		n := AINodeOf(e)
		if n == nil || e.IsDeleted {
			return nil, fmt.Errorf("模型连线的来源已删除")
		}
		switch n.Role {
		case AIModel:
			if n.Model == "" {
				return nil, fmt.Errorf("请在加载底模节点选择模型")
			}
			if n.Connection != "comfyui-zimage" && n.Connection != "comfyui-qwen-image" {
				return nil, fmt.Errorf("模型连线只支持 ComfyUI 底模")
			}
			return &AIModelConfig{Connection: n.Connection, Model: n.Model}, nil
		case AILoRA:
			if n.LinkAt("model") == "" {
				return nil, fmt.Errorf("请将底模的模型输出连接到 LoRA 的模型输入")
			}
			flow, err := visit(n.LinkAt("model"))
			if err != nil {
				return nil, err
			}
			if n.Model == "" {
				return nil, fmt.Errorf("请选择 LoRA")
			}
			if n.Connection != flow.Connection {
				return nil, fmt.Errorf("LoRA 与连接的底模不匹配")
			}
			flow.LoRAs = append(flow.LoRAs, AILoRAConfig{ID: n.Model, Strength: n.Params.LoRAStrength})
			return flow, nil
		default:
			return nil, fmt.Errorf("模型输入只能连接底模或 LoRA 节点")
		}
	}
	n := AINodeOf(s.ElementByID(id))
	if n == nil {
		return nil, nil
	}
	if n.Role == AIModel || n.Role == AILoRA {
		return visit(id)
	}
	if from := n.LinkAt("model"); from != "" {
		return visit(from)
	}
	return nil, nil
}
