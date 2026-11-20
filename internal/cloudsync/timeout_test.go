package cloudsync

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Headers alone must not let a stalled body consume the whole sync deadline.
func TestRemoteClientTimesOutStalledBody(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	client := remoteClient(false, 40*time.Millisecond)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() || ctx.Err() != nil {
		t.Fatalf("body must time out before overall deadline: error=%v context=%v", err, ctx.Err())
	}
}

func TestRemoteClientAllowsActiveTransferBeyondIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		for range 6 {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
			if _, err := io.WriteString(w, "x"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	client := remoteClient(false, 200*time.Millisecond)
	defer client.CloseIdleConnections()
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || string(data) != "xxxxxx" {
		t.Fatalf("active transfer interrupted: %q %v", data, err)
	}
}
