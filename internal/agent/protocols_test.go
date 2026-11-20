package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderErrorsDoNotEchoCredentials(t *testing.T) {
	for _, protocol := range []string{Chat, Responses, Anthropic, Gemini, Ollama} {
		t.Run(protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"error":{"message":"Rejected secret-test-token"}}`)
			}))
			defer srv.Close()
			_, err := (Settings{BaseURL: srv.URL, Model: "m", APIKey: "secret-test-token", Protocol: protocol}).NewProvider().Complete(context.Background(), Request{}, nil)
			var api *APIError
			if !errors.As(err, &api) || strings.Contains(err.Error(), "secret-test-token") || !strings.Contains(err.Error(), "[redacted]") {
				t.Fatalf("credential echoed: %v", err)
			}
		})
	}
}

func TestProviderParametersKeepNativeShapes(t *testing.T) {
	temperature := 0.6
	for _, tc := range []struct {
		protocol string
		params   Parameters
		key      string
	}{
		{Responses, Parameters{MaxOutput: 8192, Reasoning: "high"}, "reasoning"},
		{Anthropic, Parameters{MaxOutput: 8192, Reasoning: "high", ThinkingBudget: 2048}, "thinking"},
		{Gemini, Parameters{Temperature: &temperature, Reasoning: "low"}, "generationConfig"},
		{Ollama, Parameters{MaxOutput: 4096, Reasoning: "high"}, "think"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			s := Settings{BaseURL: "https://example.test/v1", Model: "m", Protocol: tc.protocol, Parameters: tc.params}
			body, _, err := (&nativeProvider{settings: s}).body(Request{Messages: []Message{{Role: User, Text: "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			if body[tc.key] == nil {
				t.Fatalf("missing native parameter: %+v", body)
			}
			if tc.protocol == Anthropic && object(body["thinking"])["budget_tokens"] != 2048 {
				t.Fatal("lost thinking budget")
			}
		})
	}
}

func TestNativeProtocolsStreamAndEncodeToolResults(t *testing.T) {
	for _, tc := range []struct{ protocol, path, stream string }{
		{Responses, "/responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"},
		{Anthropic, "/messages", "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\ndata: {\"type\":\"message_stop\"}\n\n"},
		{Gemini, "/models/model:streamGenerateContent", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2}}\n\n"},
		{Ollama, "/api/chat", "{\"message\":{\"role\":\"assistant\",\"content\":\"hello\"},\"done\":false}\n{\"done\":true,\"done_reason\":\"stop\",\"prompt_eval_count\":5,\"eval_count\":2}\n"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(body)
				if !strings.Contains(string(encoded), "tool result") {
					t.Errorf("lost result: %s", encoded)
				}
				if tc.protocol == Anthropic && (r.Header.Get("x-api-key") != "key" || number(body["max_tokens"]) == 0) {
					t.Error("invalid Anthropic request")
				}
				if tc.protocol == Gemini && r.Header.Get("x-goog-api-key") != "key" {
					t.Error("invalid Gemini auth")
				}
				fmt.Fprint(w, tc.stream)
			}))
			defer srv.Close()
			settings := Settings{BaseURL: srv.URL, Model: "model", APIKey: "key", Protocol: tc.protocol}
			var shown strings.Builder
			response, err := settings.NewProvider().Complete(context.Background(), Request{System: "system", Messages: []Message{
				{Role: User, Text: "question"}, {Role: Assistant, Calls: []ToolCall{{ID: "call", Name: "read", Args: `{"path":"a.md"}`}}}, {Role: ToolRole, CallID: "call", Text: "tool result"},
			}}, func(d Delta) { shown.WriteString(d.Text) })
			if err != nil || response.Message.Text != "hello" || shown.String() != "hello" || response.FinishReason != "stop" {
				t.Fatalf("response %+v, shown %s, err %v", response, shown.String(), err)
			}
		})
	}
}

func TestChatReportsTruncationAndKeepsPartialOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"partial"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()
	res, err := (Settings{BaseURL: srv.URL, Model: "m"}).NewProvider().Complete(context.Background(), Request{}, func(Delta) {})
	if err == nil || res.FinishReason != "length" {
		t.Fatalf("truncation accepted: %+v %v", res, err)
	}
}

func TestNativeToolCallsAndSignedReasoningRoundTrip(t *testing.T) {
	for _, tc := range []struct{ protocol, stream string }{
		{Anthropic, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"consider\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"signed\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"native-call\",\"name\":\"read\",\"input\":{}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"a.md\\\"}\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"},
		{Gemini, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"consider\",\"thought\":true},{\"functionCall\":{\"name\":\"read\",\"args\":{\"path\":\"a.md\"}},\"thoughtSignature\":\"signed\"}]},\"finishReason\":\"STOP\"}]}\n\n"},
		{Responses, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"id\":\"r\",\"encrypted_content\":\"signed\",\"summary\":[]},{\"type\":\"function_call\",\"call_id\":\"native-call\",\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"a.md\\\"}\"}]}}\n\n"},
		{Ollama, "{\"message\":{\"role\":\"assistant\",\"thinking\":\"consider\",\"tool_calls\":[{\"function\":{\"name\":\"read\",\"arguments\":{\"path\":\"a.md\"}}}]},\"done\":true,\"done_reason\":\"stop\"}\n"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.stream) }))
			defer srv.Close()
			s := Settings{BaseURL: srv.URL, Model: "m", Protocol: tc.protocol}
			res, err := s.NewProvider().Complete(context.Background(), Request{}, func(Delta) {})
			if err != nil || len(res.Message.Calls) != 1 || res.Message.Calls[0].Name != "read" || res.Message.Calls[0].Args != `{"path":"a.md"}` || res.FinishReason != "tool_calls" {
				t.Fatalf("native calls %+v %v", res, err)
			}
			request := Request{Messages: []Message{{Role: User, Text: "q"}, res.Message, {Role: ToolRole, CallID: res.Message.Calls[0].ID, Text: "result"}}}
			body, _, err := (&nativeProvider{settings: s}).body(request)
			encoded, _ := json.Marshal(body)
			if err != nil || tc.protocol != Ollama && !strings.Contains(string(encoded), "signed") {
				t.Fatalf("signed history lost %s %v", encoded, err)
			}
			neutral := NeutralHistory(request.Messages)
			for _, m := range neutral {
				if m.Source != "" || len(m.Native) > 0 || m.Reasoning != "" || len(m.Calls) > 0 || m.Role == ToolRole {
					t.Fatal("provider-specific payload survived switching")
				}
			}
		})
	}
}

func TestNativeCancellationAndIncompleteResponses(t *testing.T) {
	for _, protocol := range []string{Responses, Anthropic, Gemini, Ollama} {
		t.Run(protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "") }))
			defer srv.Close()
			s := Settings{BaseURL: srv.URL, Model: "m", Protocol: protocol}
			if _, err := s.NewProvider().Complete(context.Background(), Request{}, func(Delta) {}); err == nil {
				t.Fatal("missing completion accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := s.NewProvider().Complete(ctx, Request{}, func(Delta) {}); err == nil {
				t.Fatal("cancelled request accepted")
			}
		})
	}
}
