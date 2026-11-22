package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abhixops/homeland/internal/config"
	"github.com/abhixops/homeland/internal/health"
	"github.com/abhixops/homeland/internal/icons"
	"github.com/abhixops/homeland/internal/metrics"
	"github.com/gofiber/fiber/v2"
)

const testConfig = `
settings:
  title: "Test Lab"
groups:
  - name: Media
    apps:
      - name: Jellyfin
        url: http://127.0.0.1:1
        description: Media server
`

// newTestApp builds the full router with real templates. Icons are left
// empty so no request goes to the icon CDN.
func newTestApp(t *testing.T) (*fiber.App, string) {
	t.Helper()
	app, cfgPath, _ := newTestAppWithHandler(t)
	return app, cfgPath
}

func newTestAppWithHandler(t *testing.T) (*fiber.App, string, *Handler) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "homepage.yaml")
	if err := os.WriteFile(cfgPath, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(mgr, health.NewChecker(30, 1), icons.NewFetcher(t.TempDir()), nil,
		metrics.NewCollector(10, nil), filepath.Join("..", "..", "web", "templates"))
	h.Version = "v1.2.3"
	h.AssetVersion = "abc123def456"
	app := fiber.New()
	h.RegisterRoutes(app)
	return app, cfgPath, h
}

func get(t *testing.T, app *fiber.App, method, path string) (int, string) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(method, path, nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestIndexRenders(t *testing.T) {
	app, _ := newTestApp(t)
	status, body := get(t, app, fiber.MethodGet, "/")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	for _, want := range []string{"Test Lab", "Jellyfin", "v1.2.3", "Status unknown", "Disk",
		"/static/css/tailwind.css?v=abc123def456", "/static/js/app.js?v=abc123def456"} {
		if !strings.Contains(body, want) {
			t.Errorf("index page missing %q", want)
		}
	}
}

func TestPartialsRender(t *testing.T) {
	app, _ := newTestApp(t)
	for _, path := range []string{"/partial/grid", "/partial/health", "/partial/metrics", "/partial/calendar", "/partial/search?q=jelly"} {
		status, body := get(t, app, fiber.MethodGet, path)
		if status != 200 || strings.Contains(body, "template render error") {
			t.Errorf("%s: status %d, body %.80q", path, status, body)
		}
	}
}

func TestAPIApps(t *testing.T) {
	app, _ := newTestApp(t)
	_, body := get(t, app, fiber.MethodGet, "/api/apps")
	var got struct {
		Groups []config.Group `json:"groups"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].Apps[0].Name != "Jellyfin" {
		t.Fatalf("unexpected groups: %+v", got.Groups)
	}
}

func TestAPIVersion(t *testing.T) {
	app, _ := newTestApp(t)
	if _, body := get(t, app, fiber.MethodGet, "/api/version"); !strings.Contains(body, `"v1.2.3"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestAPIReloadReadsFileAndRejectsInvalid(t *testing.T) {
	app, cfgPath := newTestApp(t)

	updated := strings.Replace(testConfig, "Test Lab", "Renamed Lab", 1)
	if err := os.WriteFile(cfgPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _ := get(t, app, fiber.MethodPost, "/api/reload"); status != 200 {
		t.Fatalf("reload status = %d", status)
	}
	if _, body := get(t, app, fiber.MethodGet, "/"); !strings.Contains(body, "Renamed Lab") {
		t.Fatal("reload did not pick up the new title")
	}

	if err := os.WriteFile(cfgPath, []byte("groups: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _ := get(t, app, fiber.MethodPost, "/api/reload"); status != 400 {
		t.Fatalf("invalid config reload status = %d, want 400", status)
	}
}

func TestHistoryBarToggle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	app, cfgPath, h := newTestAppWithHandler(t)
	writeConfig := func(showHistory bool) {
		t.Helper()
		cfg := fmt.Sprintf(`
settings:
  health_check:
    show_history: %t
groups:
  - name: Web
    apps:
      - name: Site
        url: %s
        healthcheck: {type: http, endpoint: /}
`, showHistory, srv.URL)
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		if status, _ := get(t, app, fiber.MethodPost, "/api/reload"); status != 200 {
			t.Fatalf("reload status = %d", status)
		}
	}

	// Run a real check so the app has history to draw.
	writeConfig(false)
	h.HealthChecker.Start(h.ConfigMgr.Get().Groups[0].Apps)
	defer h.HealthChecker.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for len(h.HealthChecker.GetStatus("Site").History) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("health check never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}

	const bar = "bg-emerald-500/70"
	if _, body := get(t, app, fiber.MethodGet, "/partial/grid"); strings.Contains(body, bar) {
		t.Fatal("history bar shown with show_history: false")
	}
	writeConfig(true)
	if _, body := get(t, app, fiber.MethodGet, "/partial/grid"); !strings.Contains(body, bar) {
		t.Fatal("history bar missing with show_history: true")
	}
}

func TestHealthTitle(t *testing.T) {
	tests := []struct {
		want string
		in   health.Result
	}{
		{"Online · 42 ms · 50% up over last 2 checks", health.Result{Status: health.StatusUp, Latency: 42, History: []bool{true, false}}},
		{"Offline · HTTP 502", health.Result{Status: health.StatusDown, Error: "HTTP 502"}},
		{"Status unknown", health.Result{}},
	}
	for _, tt := range tests {
		if got := healthTitle(tt.in); got != tt.want {
			t.Errorf("healthTitle() = %q, want %q", got, tt.want)
		}
	}
}
