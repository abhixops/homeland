// Package health provides concurrent service health checking for HTTP and TCP endpoints.
package health

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/abhixops/homeland/internal/config"
)

// Status represents the health state of a service.
type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// HistorySize is the number of recent check outcomes kept per app.
const HistorySize = 30

// Result holds the health check result for a single app.
type Result struct {
	CheckedAt time.Time `json:"checked_at"`
	AppName   string    `json:"app_name"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	// History holds the most recent check outcomes, oldest first (true = up).
	History []bool `json:"history,omitempty"`
	// Latency is the response time in milliseconds.
	Latency int64 `json:"latency_ms"`
}

// UptimePercent returns the share of recorded checks that were up, or -1
// when no checks have been recorded yet.
func (r Result) UptimePercent() float64 {
	if len(r.History) == 0 {
		return -1
	}
	up := 0
	for _, ok := range r.History {
		if ok {
			up++
		}
	}
	return float64(up) * 100 / float64(len(r.History))
}

// TransitionFunc is called when an app flips between up and down.
type TransitionFunc func(app config.App, previous, current Result)

// Checker runs periodic health checks against configured endpoints.
type Checker struct {
	httpClient   *http.Client
	cancel       context.CancelFunc
	onTransition TransitionFunc
	results      map[string]Result
	interval     time.Duration
	timeout      time.Duration
	mu           sync.RWMutex // guards results
	runMu        sync.Mutex   // guards cancel, interval, timeout, httpClient, onTransition
}

// NewChecker creates a health checker with the given interval and timeout (in seconds).
func NewChecker(intervalSec, timeoutSec int) *Checker {
	c := &Checker{results: make(map[string]Result)}
	c.Configure(intervalSec, timeoutSec)
	return c
}

// Configure updates the check interval and timeout (in seconds). The new
// values take effect on the next call to Start.
func (c *Checker) Configure(intervalSec, timeoutSec int) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.interval = time.Duration(intervalSec) * time.Second
	c.timeout = time.Duration(timeoutSec) * time.Second
	c.httpClient = &http.Client{
		Timeout: c.timeout,
		// Don't follow redirects — we just need to know if the service responds
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// OnTransition registers a callback fired when an app changes between up and down.
func (c *Checker) OnTransition(fn TransitionFunc) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.onTransition = fn
}

// Start (re)starts health check goroutines for the given apps, cancelling any
// checks already running. Apps without a health check are skipped, and stored
// results for apps no longer in the list are dropped.
func (c *Checker) Start(apps []config.App) {
	c.runMu.Lock()
	defer c.runMu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	keep := make(map[string]bool, len(apps))
	r := runner{client: c.httpClient, interval: c.interval, timeout: c.timeout, onTransition: c.onTransition}
	for _, app := range apps {
		if app.HealthCheck == nil {
			continue
		}
		keep[app.Name] = true
		go c.runCheck(ctx, r, app)
	}

	c.mu.Lock()
	for name := range c.results {
		if !keep[name] {
			delete(c.results, name)
		}
	}
	c.mu.Unlock()

	log.Printf("[health] started health checks for %d apps", len(keep))
}

// Stop cancels all running health check goroutines.
func (c *Checker) Stop() {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// GetStatus returns the last known health status for an app.
func (c *Checker) GetStatus(appName string) Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.results[appName]; ok {
		return r.clone()
	}
	return Result{AppName: appName, Status: StatusUnknown}
}

// GetAll returns the health status for all checked apps.
func (c *Checker) GetAll() []Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	results := make([]Result, 0, len(c.results))
	for _, r := range c.results {
		results = append(results, r.clone())
	}
	return results
}

func (r Result) clone() Result {
	r.History = append([]bool(nil), r.History...)
	return r
}

// runner holds the settings captured when a round of checks is started, so
// a later Configure call never races with checks already in flight.
type runner struct {
	client       *http.Client
	onTransition TransitionFunc
	interval     time.Duration
	timeout      time.Duration
}

// runCheck periodically checks a single app's health.
func (c *Checker) runCheck(ctx context.Context, r runner, app config.App) {
	// Run immediately on start
	c.check(ctx, r, app)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(ctx, r, app)
		}
	}
}

// check performs a single health check and records the result.
func (c *Checker) check(ctx context.Context, r runner, app config.App) {
	start := time.Now()
	var status Status
	var errMsg string

	switch app.HealthCheck.Type {
	case "http":
		status, errMsg = r.checkHTTP(ctx, app)
	case "tcp":
		status, errMsg = r.checkTCP(ctx, app)
	default:
		status = StatusUnknown
		errMsg = fmt.Sprintf("unknown check type: %s", app.HealthCheck.Type)
	}

	// A cancelled round must not record a bogus "down" result.
	if ctx.Err() != nil {
		return
	}

	current := Result{
		AppName:   app.Name,
		Status:    status,
		Latency:   time.Since(start).Milliseconds(),
		CheckedAt: time.Now(),
		Error:     errMsg,
	}

	c.mu.Lock()
	previous, seen := c.results[app.Name]
	current.History = previous.History
	if status != StatusUnknown {
		current.History = append(current.History, status == StatusUp)
		if len(current.History) > HistorySize {
			current.History = current.History[len(current.History)-HistorySize:]
		}
	}
	c.results[app.Name] = current
	c.mu.Unlock()

	if seen && r.onTransition != nil && isFlip(previous.Status, status) {
		r.onTransition(app, previous.clone(), current.clone())
	}
}

func isFlip(prev, cur Status) bool {
	return (prev == StatusUp && cur == StatusDown) || (prev == StatusDown && cur == StatusUp)
}

// checkHTTP performs an HTTP GET to the app's base URL + health endpoint.
func (r runner) checkHTTP(ctx context.Context, app config.App) (Status, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.URL+app.HealthCheck.Endpoint, http.NoBody)
	if err != nil {
		return StatusDown, err.Error()
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Drop the `Get "<url>":` prefix — the URL is already shown on the card.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return StatusDown, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return StatusUp, ""
	}
	return StatusDown, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// checkTCP attempts a TCP dial to the health check endpoint.
func (r runner) checkTCP(ctx context.Context, app config.App) (Status, string) {
	dialer := net.Dialer{Timeout: r.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", app.HealthCheck.Endpoint)
	if err != nil {
		return StatusDown, err.Error()
	}
	conn.Close()
	return StatusUp, ""
}
