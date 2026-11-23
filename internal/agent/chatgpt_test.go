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

type fixedChatGPTToken struct{ token string }

func (s fixedChatGPTToken) AccessToken(context.Context) (string, error) { return s.token, nil }

// Account catalogs use a different envelope and must retain the provider order.
func TestChatGPTAccountModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer account-token" {
			t.Error("wrong account or endpoint")
		}
		fmt.Fprint(w, `{"models":[{"slug":"z","display_name":"First","visibility":"list"},{"slug":"hidden","visibility":"hide"},{"slug":"a","display_name":"Second","visibility":"list"}]}`)
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL, Protocol: ChatGPT, TokenSource: fixedChatGPTToken{"account-token"}}
	models, err := s.DiscoverModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "z" || models[0].Name != "First" || models[1].ID != "a" {
		t.Fatalf("account catalog: %+v %v", models, err)
	}
}

func TestChatGPTResponsesToolsAndRestrictions(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer account-token" || b["store"] != false || b["stream"] != true {
			t.Error("invalid inference route")
		}
		for _, key := range []string{"max_output_tokens", "temperature", "previous_response_id"} {
			if _, ok := b[key]; ok {
				t.Errorf("unsupported field %s", key)
			}
		}
		tool := object(b["tools"].([]any)[0])
		if tool["type"] != "namespace" || tool["name"] != "onebot" || object(tool["tools"].([]any)[0])["name"] != "read" {
			t.Error("file tools lack namespace")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"namespace\":\"onebot\",\"call_id\":\"c1\",\"name\":\"read\",\"arguments\":\"{}\"}]}}\n\n")
		} else {
			input := b["input"].([]any)
			if len(input) != 3 || object(input[1])["namespace"] != "onebot" || object(input[2])["call_id"] != "c1" {
				t.Error("lost namespaced tool history")
			}
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
		}
	}))
	defer srv.Close()
	s := Settings{ConnectionID: "first", BaseURL: srv.URL, Model: "m", Protocol: ChatGPT, TokenSource: fixedChatGPTToken{"account-token"}}
	req := Request{System: "Edit safely", Messages: []Message{{Role: User, Text: "read"}}, Tools: []ToolSpec{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	resp, err := s.NewProvider().Complete(context.Background(), req, nil)
	if err != nil || len(resp.Message.Calls) != 1 || resp.Message.Calls[0].Name != "read" {
		t.Fatalf("tool response: %+v %v", resp, err)
	}
	req.Messages = append(req.Messages, resp.Message, Message{Role: ToolRole, CallID: "c1", Text: "note"})
	resp, err = s.NewProvider().Complete(context.Background(), req, nil)
	if err != nil || resp.Message.Text != "done" {
		t.Fatalf("tool round trip: %+v %v", resp, err)
	}
	s.Parameters.MaxOutput = 200
	if _, err = s.NewProvider().Complete(context.Background(), req, nil); err == nil || requests != 2 {
		t.Fatal("unsupported settings were silently accepted")
	}
}

func TestChatGPTAdmissionDetailsAndNoAPIKeyFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-token" {
			t.Error("used API key instead of OAuth")
		}
		w.Header().Set("x-request-id", "admission-test")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"detail":"oauth-token not accepted"}`)
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL, Protocol: ChatGPT, APIKey: "unrelated-key", APIKeys: []string{"one", "two"}, TokenSource: fixedChatGPTToken{"oauth-token"}}
	_, err := s.DiscoverModels(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.Status != 401 || api.Shape != "detail" || api.RequestID != "admission-test" || api.Protocol != ChatGPT || api.Message != "[redacted] not accepted" {
		t.Fatalf("lost admission diagnostics: %+v", api)
	}
}

func TestChatGPTRequiresCompletedStreamAndRejectsAudio(t *testing.T) {
	for _, stream := range []string{"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", "data: {\"type\":\"response.incomplete\",\"response\":{\"incomplete_details\":{\"reason\":\"unknown_reason\"}}}\n\n"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, stream) }))
		s := Settings{BaseURL: srv.URL, Model: "m", Protocol: ChatGPT, TokenSource: fixedChatGPTToken{"test"}}
		if _, err := s.NewProvider().Complete(context.Background(), Request{}, nil); err == nil {
			t.Fatal("partial stream accepted")
		}
		if _, err := s.NewProvider().Complete(context.Background(), Request{Messages: []Message{{Role: User, Parts: []ContentPart{{Type: "audio", MIME: "audio/wav", Data: "data:audio/wav;base64,AA=="}}}}}, nil); err == nil {
			t.Fatal("unsupported audio accepted")
		}
		srv.Close()
	}
}

func TestChatGPTStreamFailurePreservesUsageCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "request-test")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\",\"message\":\"limit\",\"param\":\"model\"}}}\n\n")
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL, Model: "m", Protocol: ChatGPT, TokenSource: fixedChatGPTToken{"account-token"}}
	_, err := s.NewProvider().Complete(context.Background(), Request{}, nil)
	var api *APIError
	if !errors.As(err, &api) || api.Code != "subscription_sharing_usage_limit_exceeded" || api.RequestID != "request-test" || api.Param != "model" {
		t.Fatalf("lost recovery details: %+v", err)
	}
}

func TestChatGPTErrorsRedactCredentialsInEveryDiagnosticField(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "synthetic-token")
				if stream {
					fmt.Fprint(w, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"synthetic-token\",\"code\":\"synthetic-token\",\"param\":\"synthetic-token\"}}}\n\n")
				} else {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"synthetic-token","code":"synthetic-token","param":"synthetic-token"}}`)
				}
			}))
			defer srv.Close()
			s := Settings{BaseURL: srv.URL, Model: "m", Protocol: ChatGPT, TokenSource: fixedChatGPTToken{"synthetic-token"}}
			_, err := s.NewProvider().Complete(context.Background(), Request{}, nil)
			if err == nil {
				t.Fatal("missing error")
			}
			raw, _ := json.Marshal(err)
			if strings.Contains(string(raw), "synthetic-token") {
				t.Fatal("credential in diagnostic")
			}
		})
	}
}
