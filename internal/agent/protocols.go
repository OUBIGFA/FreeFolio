package agent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var ErrIncomplete = errors.New("the model did not finish the response")

func completionError(reason string) error {
	switch reason {
	case "length", "max_tokens", "MAX_TOKENS":
		return fmt.Errorf("%w: output limit reached", ErrIncomplete)
	case "content_filter", "refusal", "SAFETY", "RECITATION", "blocked":
		return fmt.Errorf("%w: response blocked by the provider", ErrIncomplete)
	}
	return nil
}

func validateParameters(p Parameters, c Capabilities, req Request) error {
	if p.MaxOutput < 0 || c.MaxOutput > 0 && p.MaxOutput > c.MaxOutput {
		return errors.New("requested output exceeds the model limit")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2 || c.Temperature == Unsupported) {
		return errors.New("temperature is not supported or is outside 0–2")
	}
	if p.Reasoning != "" && p.Reasoning != "off" && (c.Reasoning == Unsupported || len(c.Efforts) > 0 && !slices.Contains(c.Efforts, p.Reasoning)) {
		return errors.New("the model does not support the selected reasoning level")
	}
	if len(req.Tools) > 0 && c.Tools == Unsupported {
		return errors.New("the selected model does not support tool calls")
	}
	for _, m := range req.Messages {
		for _, part := range m.Parts {
			modality := part.Type
			if modality == "text" {
				continue
			}
			if modality == "file" && part.MIME == "application/pdf" {
				modality = "pdf"
			}
			if len(c.Input) > 0 && !c.Accepts(modality) {
				return fmt.Errorf("the model does not accept %s input", modality)
			}
		}
	}
	return nil
}

func (s Settings) Signature() string {
	return s.ConnectionID + "|" + s.Protocol + "|" + strings.TrimRight(s.BaseURL, "/") + "|" + s.Model
}

// NewProvider selects an actual wire adapter, not merely a different URL.
// Several API keys share one connection through a key pool.
func (s Settings) NewProvider() Provider {
	if len(s.APIKeys) > 1 {
		return &keyPool{settings: s, keys: s.keyOrder()}
	}
	return s.wireProvider()
}

func (s Settings) wireProvider() Provider {
	if s.Protocol == "" || s.Protocol == Chat {
		return s.Provider()
	}
	return &nativeProvider{settings: s}
}

type nativeProvider struct{ settings Settings }

func decodeNative(m Message, s Settings) []any {
	if m.Source != s.Signature() || len(m.Native) == 0 {
		return nil
	}
	var parts []any
	if json.Unmarshal(m.Native, &parts) != nil {
		return nil
	}
	return parts
}

func dataPart(p ContentPart) (string, string, error) {
	mime := p.MIME
	data := p.Data
	if strings.HasPrefix(data, "data:") {
		header, value, ok := strings.Cut(strings.TrimPrefix(data, "data:"), ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return "", "", errors.New("invalid attachment data")
		}
		mime = strings.TrimSuffix(header, ";base64")
		data = value
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", errors.New("attachments must contain base64 data")
	}
	if mime == "" {
		return "", "", errors.New("attachment media type is missing")
	}
	return mime, data, nil
}

func (p *nativeProvider) Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	s := p.settings
	if onDelta == nil {
		onDelta = func(Delta) {}
	}
	if err := validateParameters(s.Parameters, s.Capabilities, req); err != nil {
		return Response{}, err
	}
	body, endpoint, err := p.body(req)
	if err != nil {
		return Response{}, err
	}
	res, err := s.request(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	state := nativeStream{protocol: s.Protocol, emit: onDelta, response: Response{Message: Message{Role: Assistant, Source: s.Signature()}}, calls: map[int]*ToolCall{}, blocks: map[int]map[string]any{}}
	if s.Protocol == Ollama {
		err = readJSONLines(res.Body, state.consume)
	} else {
		err = readSSE(res.Body, state.consume)
	}
	if ctx.Err() != nil {
		return state.response, ctx.Err()
	}
	if err != nil {
		return state.response, err
	}
	if !state.finished {
		return state.response, ErrStreamCut
	}
	state.finish()
	return state.response, completionError(state.response.FinishReason)
}

func (p *nativeProvider) body(req Request) (map[string]any, string, error) {
	s := p.settings
	base := strings.TrimRight(s.BaseURL, "/")
	maxOut := s.Parameters.MaxOutput
	if maxOut == 0 {
		maxOut = 4096
		if s.Capabilities.MaxOutput > 0 {
			maxOut = min(maxOut, s.Capabilities.MaxOutput)
		}
	}
	calls := map[string]string{}
	for _, m := range req.Messages {
		for _, call := range m.Calls {
			calls[call.ID] = call.Name
		}
	}
	tools := make([]any, 0, len(req.Tools))
	messages := make([]any, 0, len(req.Messages))
	switch s.Protocol {
	case Responses:
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": false})
		}
		for _, m := range req.Messages {
			if native := decodeNative(m, s); len(native) > 0 {
				messages = append(messages, native...)
				continue
			}
			if m.Role == ToolRole {
				messages = append(messages, map[string]any{"type": "function_call_output", "call_id": m.CallID, "output": m.Text})
				continue
			}
			if m.Text != "" || len(m.Parts) > 0 {
				parts := []any{}
				typ := "input_text"
				if m.Role == Assistant {
					typ = "output_text"
				}
				if m.Text != "" {
					parts = append(parts, map[string]any{"type": typ, "text": m.Text})
				}
				for _, part := range m.Parts {
					switch part.Type {
					case "text":
						parts = append(parts, map[string]any{"type": "input_text", "text": part.Text})
					case "image":
						parts = append(parts, map[string]any{"type": "input_image", "image_url": part.Data})
					case "file":
						parts = append(parts, map[string]any{"type": "input_file", "filename": part.Name, "file_data": part.Data})
					default:
						return nil, "", errors.New("unsupported attachment")
					}
				}
				messages = append(messages, map[string]any{"role": string(m.Role), "content": parts})
			}
			for _, call := range m.Calls {
				messages = append(messages, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": call.Args})
			}
		}
		body := map[string]any{"model": s.Model, "input": messages, "stream": true, "store": false, "instructions": req.System, "include": []string{"reasoning.encrypted_content"}}
		if len(tools) > 0 {
			body["tools"] = tools
		}
		if s.Parameters.MaxOutput > 0 {
			body["max_output_tokens"] = s.Parameters.MaxOutput
		}
		if s.Parameters.Temperature != nil {
			body["temperature"] = *s.Parameters.Temperature
		}
		if s.Parameters.Reasoning != "" && s.Parameters.Reasoning != "off" {
			body["reasoning"] = map[string]any{"effort": s.Parameters.Reasoning, "summary": "auto"}
		}
		if s.Parameters.Reasoning == "off" {
			body["reasoning"] = map[string]any{"effort": "none"}
		}
		return body, base + "/responses", nil
	case Anthropic:
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
		}
		for _, m := range req.Messages {
			role := string(m.Role)
			parts := decodeNative(m, s)
			if len(parts) == 0 {
				if m.Role == ToolRole {
					role = "user"
					parts = append(parts, map[string]any{"type": "tool_result", "tool_use_id": m.CallID, "content": m.Text, "is_error": m.IsError})
				} else {
					if m.Text != "" {
						parts = append(parts, map[string]any{"type": "text", "text": m.Text})
					}
					for _, part := range m.Parts {
						if part.Type == "text" {
							parts = append(parts, map[string]any{"type": "text", "text": part.Text})
							continue
						}
						mime, data, err := dataPart(part)
						if err != nil {
							return nil, "", err
						}
						if part.Type != "image" && !(part.Type == "file" && mime == "application/pdf") {
							return nil, "", errors.New("Anthropic supports image and PDF attachments")
						}
						typ := "image"
						if part.Type == "file" {
							typ = "document"
						}
						parts = append(parts, map[string]any{"type": typ, "source": map[string]any{"type": "base64", "media_type": mime, "data": data}})
					}
					for _, call := range m.Calls {
						var args any
						if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
							return nil, "", err
						}
						parts = append(parts, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": args})
					}
				}
			}
			if len(parts) == 0 {
				continue
			}
			// Consecutive tool results form one user message in Messages API.
			if len(messages) > 0 && object(messages[len(messages)-1])["role"] == role {
				last := object(messages[len(messages)-1])
				last["content"] = append(last["content"].([]any), parts...)
			} else {
				messages = append(messages, map[string]any{"role": role, "content": parts})
			}
		}
		body := map[string]any{"model": s.Model, "messages": messages, "max_tokens": maxOut, "stream": true}
		if req.System != "" {
			body["system"] = req.System
		}
		if len(tools) > 0 {
			body["tools"] = tools
		}
		if s.Parameters.Temperature != nil {
			body["temperature"] = *s.Parameters.Temperature
		}
		if s.Parameters.Reasoning != "" && s.Parameters.Reasoning != "off" {
			if s.Parameters.ThinkingBudget > 0 {
				if s.Parameters.ThinkingBudget >= maxOut {
					return nil, "", errors.New("thinking budget must be below the output limit")
				}
				body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": s.Parameters.ThinkingBudget}
			} else {
				body["thinking"] = map[string]any{"type": "adaptive"}
				body["output_config"] = map[string]any{"effort": s.Parameters.Reasoning}
			}
		}
		if s.Parameters.Reasoning == "off" {
			body["thinking"] = map[string]any{"type": "disabled"}
		}
		return body, base + "/messages", nil
	case Gemini:
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "parametersJsonSchema": t.Parameters})
		}
		for _, m := range req.Messages {
			role := "user"
			if m.Role == Assistant {
				role = "model"
			}
			parts := decodeNative(m, s)
			if len(parts) == 0 {
				if m.Role == ToolRole {
					parts = append(parts, map[string]any{"functionResponse": map[string]any{"name": calls[m.CallID], "response": map[string]any{"result": m.Text, "is_error": m.IsError}}})
				} else {
					if m.Text != "" {
						parts = append(parts, map[string]any{"text": m.Text})
					}
					for _, part := range m.Parts {
						if part.Type == "text" {
							parts = append(parts, map[string]any{"text": part.Text})
							continue
						}
						mime, data, err := dataPart(part)
						if err != nil {
							return nil, "", err
						}
						parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}})
					}
					for _, call := range m.Calls {
						var args any
						if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
							return nil, "", err
						}
						parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": args}})
					}
				}
			}
			if len(parts) > 0 {
				messages = append(messages, map[string]any{"role": role, "parts": parts})
			}
		}
		body := map[string]any{"contents": messages}
		if req.System != "" {
			body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": req.System}}}
		}
		if len(tools) > 0 {
			body["tools"] = []any{map[string]any{"functionDeclarations": tools}}
		}
		config := map[string]any{}
		if s.Parameters.MaxOutput > 0 {
			config["maxOutputTokens"] = s.Parameters.MaxOutput
		}
		if s.Parameters.Temperature != nil {
			config["temperature"] = *s.Parameters.Temperature
		}
		if s.Parameters.Reasoning != "" {
			thinking := map[string]any{"includeThoughts": true}
			if s.Parameters.Reasoning == "off" {
				thinking["thinkingBudget"] = 0
			} else if s.Parameters.ThinkingBudget > 0 {
				thinking["thinkingBudget"] = s.Parameters.ThinkingBudget
			} else {
				thinking["thinkingLevel"] = s.Parameters.Reasoning
			}
			config["thinkingConfig"] = thinking
		}
		if len(config) > 0 {
			body["generationConfig"] = config
		}
		return body, base + "/models/" + url.PathEscape(strings.TrimPrefix(s.Model, "models/")) + ":streamGenerateContent?alt=sse", nil
	case Ollama:
		if req.System != "" {
			messages = append(messages, map[string]any{"role": "system", "content": req.System})
		}
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		for _, m := range req.Messages {
			item := map[string]any{"role": string(m.Role), "content": m.Text}
			if m.Role == ToolRole {
				item["tool_name"] = calls[m.CallID]
			}
			if m.Reasoning != "" && m.Source == s.Signature() {
				item["thinking"] = m.Reasoning
			}
			var images []string
			for _, part := range m.Parts {
				if part.Type == "text" {
					item["content"] = str(item["content"]) + "\n" + part.Text
					continue
				}
				if part.Type != "image" {
					return nil, "", errors.New("Ollama chat accepts image or text attachments")
				}
				_, data, err := dataPart(part)
				if err != nil {
					return nil, "", err
				}
				images = append(images, data)
			}
			if len(images) > 0 {
				item["images"] = images
			}
			var tc []any
			for _, call := range m.Calls {
				var args any
				if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
					return nil, "", err
				}
				tc = append(tc, map[string]any{"function": map[string]any{"name": call.Name, "arguments": args}})
			}
			if len(tc) > 0 {
				item["tool_calls"] = tc
			}
			messages = append(messages, item)
		}
		body := map[string]any{"model": s.Model, "messages": messages, "stream": true}
		if len(tools) > 0 {
			body["tools"] = tools
		}
		options := map[string]any{}
		if s.Parameters.MaxOutput > 0 {
			options["num_predict"] = s.Parameters.MaxOutput
		}
		if s.Parameters.Temperature != nil {
			options["temperature"] = *s.Parameters.Temperature
		}
		if len(options) > 0 {
			body["options"] = options
		}
		if s.Parameters.Reasoning == "off" {
			body["think"] = false
		} else if s.Parameters.Reasoning != "" {
			body["think"] = s.Parameters.Reasoning
		}
		return body, base + "/api/chat", nil
	default:
		return nil, "", fmt.Errorf("unsupported provider protocol %q", s.Protocol)
	}
}

func readJSONLines(r io.Reader, consume func(map[string]any) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 8<<20)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(sc.Bytes(), &data); err != nil {
			return fmt.Errorf("invalid streamed JSON: %w", err)
		}
		if err := consume(data); err != nil {
			return err
		}
	}
	return sc.Err()
}

// readSSE supports multi-line data frames, ignores comments and does not treat
// EOF as success; the protocol's final event is required by the caller.
func readSSE(r io.Reader, consume func(map[string]any) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 8<<20)
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		if raw == "[DONE]" {
			return nil
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return fmt.Errorf("invalid stream event: %w", err)
		}
		return consume(event)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if v, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimSpace(v))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

type nativeStream struct {
	protocol string
	emit     func(Delta)
	response Response
	finished bool
	calls    map[int]*ToolCall
	blocks   map[int]map[string]any
	native   []any
}

func (s *nativeStream) text(text string, reasoning bool) {
	if text == "" {
		return
	}
	if reasoning {
		s.response.Message.Reasoning += text
		s.emit(Delta{Reasoning: text})
	} else {
		s.response.Message.Text += text
		s.emit(Delta{Text: text})
	}
}
func (s *nativeStream) call(i int) *ToolCall {
	if s.calls[i] == nil {
		s.calls[i] = &ToolCall{ID: "call_" + strconv.Itoa(i)}
	}
	return s.calls[i]
}

func (s *nativeStream) consume(e map[string]any) error {
	if errValue, ok := e["error"]; ok && errValue != nil {
		msg := str(errValue)
		if msg == "" {
			msg = str(object(errValue)["message"])
		}
		return &APIError{Status: 500, Message: msg}
	}
	switch s.protocol {
	case Responses:
		switch str(e["type"]) {
		case "response.output_text.delta":
			s.text(str(e["delta"]), false)
		case "response.reasoning_summary_text.delta":
			s.text(str(e["delta"]), true)
		case "response.output_item.added", "response.output_item.done":
			i := number(e["output_index"])
			item := object(e["item"])
			s.blocks[i] = item
			if str(item["type"]) == "function_call" {
				c := s.call(i)
				c.ID = str(item["call_id"])
				c.Name = str(item["name"])
				if args := str(item["arguments"]); args != "" {
					c.Args = args
				}
			}
		case "response.function_call_arguments.delta":
			s.call(number(e["output_index"])).Args += str(e["delta"])
		case "response.completed", "response.incomplete":
			r := object(e["response"])
			s.finished = true
			s.response.FinishReason = "stop"
			if str(e["type"]) == "response.incomplete" {
				s.response.FinishReason = str(object(r["incomplete_details"])["reason"])
				if s.response.FinishReason == "max_output_tokens" {
					s.response.FinishReason = "length"
				}
				if s.response.FinishReason == "" {
					return ErrIncomplete
				}
			}
			u := object(r["usage"])
			s.response.Usage = Usage{number(u["input_tokens"]), number(u["output_tokens"])}
			if output, ok := r["output"].([]any); ok {
				s.native = output
				for i, raw := range output {
					item := object(raw)
					if str(item["type"]) == "function_call" {
						c := s.call(i)
						c.ID = str(item["call_id"])
						c.Name = str(item["name"])
						c.Args = str(item["arguments"])
					}
				}
			}
		case "response.failed":
			return &APIError{Status: 500, Message: str(object(object(e["response"])["error"])["message"])}
		}
	case Anthropic:
		i := number(e["index"])
		switch str(e["type"]) {
		case "message_start":
			s.response.Usage.Input = number(object(object(e["message"])["usage"])["input_tokens"])
		case "content_block_start":
			b := object(e["content_block"])
			s.blocks[i] = b
			if str(b["type"]) == "tool_use" {
				c := s.call(i)
				c.ID = str(b["id"])
				c.Name = str(b["name"])
			}
		case "content_block_delta":
			d := object(e["delta"])
			b := s.blocks[i]
			if b == nil {
				b = map[string]any{}
				s.blocks[i] = b
			}
			switch str(d["type"]) {
			case "text_delta":
				s.text(str(d["text"]), false)
				b["type"] = "text"
				b["text"] = str(b["text"]) + str(d["text"])
			case "thinking_delta":
				s.text(str(d["thinking"]), true)
				b["type"] = "thinking"
				b["thinking"] = str(b["thinking"]) + str(d["thinking"])
			case "signature_delta":
				b["signature"] = str(b["signature"]) + str(d["signature"])
			case "input_json_delta":
				s.call(i).Args += str(d["partial_json"])
			}
		case "content_block_stop":
			if c := s.calls[i]; c != nil {
				if c.Args == "" {
					c.Args = "{}"
				}
				var input any
				if err := json.Unmarshal([]byte(c.Args), &input); err != nil {
					return err
				}
				s.blocks[i]["input"] = input
			}
		case "message_delta":
			reason := str(object(e["delta"])["stop_reason"])
			switch reason {
			case "end_turn", "stop_sequence":
				reason = "stop"
			case "tool_use":
				reason = "tool_calls"
			case "max_tokens":
				reason = "length"
			}
			s.response.FinishReason = reason
			s.response.Usage.Output = number(object(e["usage"])["output_tokens"])
		case "message_stop":
			s.finished = true
			if s.response.FinishReason == "" {
				s.response.FinishReason = "unknown"
			}
		}
	case Gemini:
		if feedback := object(e["promptFeedback"]); str(feedback["blockReason"]) != "" {
			return fmt.Errorf("%w: %s", ErrIncomplete, str(feedback["blockReason"]))
		}
		if candidates, ok := e["candidates"].([]any); ok && len(candidates) > 0 {
			candidate := object(candidates[0])
			if parts, ok := object(candidate["content"])["parts"].([]any); ok {
				for _, raw := range parts {
					part := object(raw)
					s.native = append(s.native, part)
					thought, _ := part["thought"].(bool)
					s.text(str(part["text"]), thought)
					if fc := object(part["functionCall"]); fc != nil {
						c := s.call(len(s.calls))
						if id := str(fc["id"]); id != "" {
							c.ID = id
						}
						c.Name = str(fc["name"])
						args, _ := json.Marshal(fc["args"])
						c.Args = string(args)
					}
				}
			}
			if reason := str(candidate["finishReason"]); reason != "" {
				s.finished = true
				switch reason {
				case "STOP":
					reason = "stop"
				case "MAX_TOKENS":
					reason = "length"
				default:
					reason = "blocked"
				}
				s.response.FinishReason = reason
			}
		}
		if u := object(e["usageMetadata"]); u != nil {
			s.response.Usage = Usage{number(u["promptTokenCount"]), number(u["candidatesTokenCount"])}
		}
	case Ollama:
		m := object(e["message"])
		s.text(str(m["content"]), false)
		s.text(str(m["thinking"]), true)
		if calls, ok := m["tool_calls"].([]any); ok {
			for _, raw := range calls {
				f := object(object(raw)["function"])
				c := s.call(len(s.calls))
				c.Name = str(f["name"])
				args, _ := json.Marshal(f["arguments"])
				c.Args = string(args)
			}
		}
		if done, _ := e["done"].(bool); done {
			s.finished = true
			s.response.FinishReason = str(e["done_reason"])
			if s.response.FinishReason == "" {
				s.response.FinishReason = "stop"
			}
			s.response.Usage = Usage{number(e["prompt_eval_count"]), number(e["eval_count"])}
		}
	}
	return nil
}

func (s *nativeStream) finish() {
	var keys []int
	for i := range s.calls {
		keys = append(keys, i)
	}
	slices.Sort(keys)
	for _, i := range keys {
		s.response.Message.Calls = append(s.response.Message.Calls, *s.calls[i])
	}
	if len(s.native) == 0 {
		keys = nil
		for i := range s.blocks {
			keys = append(keys, i)
		}
		slices.Sort(keys)
		for _, i := range keys {
			s.native = append(s.native, s.blocks[i])
		}
	}
	if len(s.native) > 0 {
		s.response.Message.Native, _ = json.Marshal(s.native)
	}
	if len(s.calls) > 0 && s.response.FinishReason == "stop" {
		s.response.FinishReason = "tool_calls"
	}
}

// NeutralHistory keeps answers and tool results when switching providers while
// removing signed or provider-specific reasoning and tool-call wire records.
func NeutralHistory(history []Message) []Message {
	out := make([]Message, 0, len(history))
	for _, m := range history {
		m.Native = nil
		m.Source = ""
		m.Reasoning = ""
		if m.Role == ToolRole {
			m.Role = User
			m.Text = "Previous tool result (reference data):\n" + m.Text
			m.CallID = ""
		}
		if len(m.Calls) > 0 {
			for _, c := range m.Calls {
				m.Text += "\nPrevious operation: " + c.Name + " " + c.Args
			}
			m.Calls = nil
		}
		out = append(out, m)
	}
	return out
}
