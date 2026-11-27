package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestNativeSSETailCutCanRetryWithoutHidingMalformedEvents(t *testing.T) {
	consume := func(map[string]any) error { return nil }
	if err := readSSE(strings.NewReader("data: {\"type\":\"response."), consume); !errors.Is(err, ErrStreamCut) {
		t.Fatalf("tail cut = %v", err)
	}
	if err := readSSE(strings.NewReader("data: not-json\n\ndata: {}\n\n"), consume); err == nil || errors.Is(err, ErrStreamCut) {
		t.Fatalf("malformed complete event = %v", err)
	}
}
