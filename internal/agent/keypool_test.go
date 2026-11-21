package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// keyServer answers chat requests, refusing the keys in refuse with status.
func keyServer(t *testing.T, status int, refuse ...string) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seen = append(seen, key)
		mu.Unlock()
		for _, k := range refuse {
			if k == key {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"refused `+key+`"}}`)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+key+"\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func poolSettings(url, strategy, id string) Settings {
	return Settings{ConnectionID: id, BaseURL: url, Model: "m", APIKey: "a", APIKeys: []string{"a", "b", "c"}, KeyStrategy: strategy, Protocol: Chat}
}

func TestKeyPoolRoundRobinSpreadsRequests(t *testing.T) {
	srv, _ := keyServer(t, 0)
	s := poolSettings(srv.URL, KeyRoundRobin, t.Name())
	var got []string
	for range 4 {
		resp, err := s.NewProvider().Complete(context.Background(), Request{Messages: []Message{{Role: "user", Text: "hi"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, resp.Message.Text)
	}
	if strings.Join(got, "") != "abca" {
		t.Fatalf("round robin used %v", got)
	}
}

func TestKeyPoolFailoverSkipsRefusedKeyOnly(t *testing.T) {
	srv, seen := keyServer(t, http.StatusTooManyRequests, "a")
	s := poolSettings(srv.URL, KeyFailover, t.Name())
	resp, err := s.NewProvider().Complete(context.Background(), Request{Messages: []Message{{Role: "user", Text: "hi"}}}, nil)
	if err != nil || resp.Message.Text != "b" || strings.Join(*seen, "") != "ab" {
		t.Fatalf("failover: %q %v, tried %v", resp.Message.Text, err, *seen)
	}

	bad, seen := keyServer(t, http.StatusBadRequest, "a")
	s = poolSettings(bad.URL, KeyFailover, t.Name()+"-bad")
	if _, err = s.NewProvider().Complete(context.Background(), Request{Messages: []Message{{Role: "user", Text: "hi"}}}, nil); err == nil || strings.Join(*seen, "") != "a" {
		t.Fatalf("a bad request is not the key's fault: %v, tried %v", err, *seen)
	}

	all, _ := keyServer(t, http.StatusUnauthorized, "a", "b", "c")
	s = poolSettings(all.URL, KeyFailover, t.Name()+"-all")
	if _, err = s.NewProvider().Complete(context.Background(), Request{Messages: []Message{{Role: "user", Text: "hi"}}}, nil); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("all keys refused: %v", err)
	}
}
