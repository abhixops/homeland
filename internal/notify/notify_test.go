package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abhixops/homeland/internal/config"
)

type captured struct {
	header http.Header
	body   []byte
}

func captureServer(t *testing.T) (*httptest.Server, <-chan captured) {
	t.Helper()
	ch := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- captured{header: r.Header.Clone(), body: body}
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func TestSendJSON(t *testing.T) {
	srv, got := captureServer(t)
	n := New(nil)
	event := Event{App: "Grafana", Status: "down", Previous: "up", Error: "HTTP 502"}

	if err := n.Send(t.Context(), config.NotifyCfg{WebhookURL: srv.URL}, event); err != nil {
		t.Fatalf("Send: %v", err)
	}
	req := <-got
	if ct := req.header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := "🔴 Grafana is down (HTTP 502)"
	for _, key := range []string{"text", "content", "message"} {
		if body[key] != want {
			t.Errorf("%s = %q, want %q", key, body[key], want)
		}
	}
	if body["app"] != "Grafana" || body["status"] != "down" {
		t.Errorf("structured fields missing: %v", body)
	}
}

func TestSendNtfy(t *testing.T) {
	srv, got := captureServer(t)
	n := New(nil)
	event := Event{App: "Grafana", Status: "up", Previous: "down"}

	if err := n.Send(t.Context(), config.NotifyCfg{WebhookURL: srv.URL, Format: "ntfy"}, event); err != nil {
		t.Fatalf("Send: %v", err)
	}
	req := <-got
	if string(req.body) != "✅ Grafana is back up" {
		t.Fatalf("body = %q", req.body)
	}
	if req.header.Get("Title") != "Homeland: Grafana" {
		t.Fatalf("Title header = %q", req.header.Get("Title"))
	}
}

func TestSendReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := New(nil).Send(t.Context(), config.NotifyCfg{WebhookURL: srv.URL}, Event{App: "x"}); err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
}
