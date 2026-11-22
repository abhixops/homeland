package health

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abhixops/homeland/internal/config"
)

func httpApp(name, url string) config.App {
	return config.App{Name: name, URL: url, HealthCheck: &config.HealthCheck{Type: "http", Endpoint: "/"}}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestCheckRecordsStatusLatencyAndHistory(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	c := NewChecker(1, 1)
	app := httpApp("web", srv.URL)
	r := runner{client: c.httpClient, interval: c.interval, timeout: c.timeout}
	ctx := t.Context()

	c.check(ctx, r, app)
	healthy.Store(false)
	c.check(ctx, r, app)

	got := c.GetStatus("web")
	if got.Status != StatusDown || got.Error != "HTTP 502" {
		t.Fatalf("got status %q error %q, want down / HTTP 502", got.Status, got.Error)
	}
	if want := []bool{true, false}; len(got.History) != 2 || got.History[0] != want[0] || got.History[1] != want[1] {
		t.Fatalf("history = %v, want %v", got.History, want)
	}
	if pct := got.UptimePercent(); pct != 50 {
		t.Fatalf("UptimePercent = %v, want 50", pct)
	}
}

func TestHistoryIsCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	c := NewChecker(1, 1)
	r := runner{client: c.httpClient, interval: c.interval, timeout: c.timeout}
	for range HistorySize + 5 {
		c.check(t.Context(), r, httpApp("web", srv.URL))
	}
	if n := len(c.GetStatus("web").History); n != HistorySize {
		t.Fatalf("history length = %d, want %d", n, HistorySize)
	}
}

func TestTransitionCallbackFiresOnFlipOnly(t *testing.T) {
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	var mu sync.Mutex
	var flips []Status
	c := NewChecker(1, 1)
	r := runner{client: c.httpClient, interval: c.interval, timeout: c.timeout,
		onTransition: func(_ config.App, _, cur Result) {
			mu.Lock()
			flips = append(flips, cur.Status)
			mu.Unlock()
		}}
	app := httpApp("web", srv.URL)

	healthy.Store(true)
	c.check(t.Context(), r, app) // first result: no previous, no alert
	c.check(t.Context(), r, app) // up → up: no alert
	healthy.Store(false)
	c.check(t.Context(), r, app) // up → down: alert
	c.check(t.Context(), r, app) // down → down: no alert
	healthy.Store(true)
	c.check(t.Context(), r, app) // down → up: alert

	mu.Lock()
	defer mu.Unlock()
	if len(flips) != 2 || flips[0] != StatusDown || flips[1] != StatusUp {
		t.Fatalf("flips = %v, want [down up]", flips)
	}
}

func TestStartChecksGivenAppsAndPrunesRemoved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	c := NewChecker(60, 1)
	defer c.Stop()

	c.Start([]config.App{httpApp("a", srv.URL), httpApp("b", srv.URL), {Name: "no-check"}})
	waitFor(t, func() bool {
		return c.GetStatus("a").Status == StatusUp && c.GetStatus("b").Status == StatusUp
	})

	c.Start([]config.App{httpApp("a", srv.URL)})
	if got := c.GetStatus("b").Status; got != StatusUnknown {
		t.Fatalf("removed app b status = %q, want unknown", got)
	}
	if got := c.GetStatus("a"); len(got.History) == 0 {
		t.Fatal("history for app a should survive a restart")
	}
}
