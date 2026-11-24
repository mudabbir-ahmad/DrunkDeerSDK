package main

import (
	"strings"
	"testing"
)

func TestServeInProcessStatus(t *testing.T) {
	kb := NewKeyboard(true, t.Logf)
	kb.Redetect()
	defer kb.Close()
	store, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newMux(kb, store, func() {})

	status, body := serveInProcess(h, "GET", "/api/status", nil)
	if status != 200 {
		t.Fatalf("GET /api/status: status %d, want 200; body %s", status, body)
	}
	if !strings.Contains(string(body), `"connected":true`) {
		t.Errorf("GET /api/status: body %s has no connected field", body)
	}
}
