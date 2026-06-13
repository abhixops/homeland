// Package health provides concurrent service health checking for HTTP and TCP endpoints.
package health

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
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

// Result holds the health check result for a single app.
type Result struct {
	CheckedAt time.Time `json:"checked_at"`
	AppName   string    `json:"app_name"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	// Latency is the response time in milliseconds.
	Latency int64 `json:"latency_ms"`
}

// Checker runs periodic health checks against configured endpoints.
type Checker struct {
	httpClient *http.Client
	cancel     context.CancelFunc
	results    sync.Map
	interval   time.Duration
	timeout    time.Duration
}

// NewChecker creates a health checker with the given interval and timeout (in seconds).
func NewChecker(intervalSec, timeoutSec int) *Checker {
	return &Checker{
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
			// Don't follow redirects — we just need to know if the service responds
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		interval: time.Duration(intervalSec) * time.Second,
		timeout:  time.Duration(timeoutSec) * time.Second,
	}
}

// Start begins health check goroutines for all apps in the config.
// Call Stop() to cancel all running checks before starting new ones.
func (c *Checker) Start(cfg *config.Config) {
	// Cancel any existing checks
	c.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	for _, group := range cfg.Groups {
		for _, app := range group.Apps {
			if app.HealthCheck == nil {
				continue
			}
			// Launch a goroutine per app for concurrent health checking
			go c.runCheck(ctx, app)
		}
	}
	log.Println("[health] started health checks")
}

// Stop cancels all running health check goroutines.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// GetStatus returns the last known health status for an app.
func (c *Checker) GetStatus(appName string) Result {
	if val, ok := c.results.Load(appName); ok {
		return val.(Result)
	}
	return Result{AppName: appName, Status: StatusUnknown}
}

// GetAll returns the health status for all checked apps.
func (c *Checker) GetAll() []Result {
	var results []Result
	c.results.Range(func(key, value interface{}) bool {
		results = append(results, value.(Result))
		return true
	})
	return results
}

// runCheck periodically checks a single app's health.
func (c *Checker) runCheck(ctx context.Context, app config.App) {
	// Run immediately on start
	c.check(app)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(app)
		}
	}
}

// check performs a single health check based on the check type.
func (c *Checker) check(app config.App) {
	start := time.Now()
	var status Status
	var errMsg string

	switch app.HealthCheck.Type {
	case "http":
		status, errMsg = c.checkHTTP(app)
	case "tcp":
		status, errMsg = c.checkTCP(app)
	default:
		status = StatusUnknown
		errMsg = fmt.Sprintf("unknown check type: %s", app.HealthCheck.Type)
	}

	latency := time.Since(start).Milliseconds()
	result := Result{
		AppName:   app.Name,
		Status:    status,
		Latency:   latency,
		CheckedAt: time.Now(),
		Error:     errMsg,
	}
	c.results.Store(app.Name, result)
}

// checkHTTP performs an HTTP GET to the app's base URL + health endpoint.
func (c *Checker) checkHTTP(app config.App) (Status, string) {
	url := app.URL + app.HealthCheck.Endpoint
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return StatusDown, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return StatusUp, ""
	}
	return StatusDown, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// checkTCP attempts a TCP dial to the health check endpoint.
func (c *Checker) checkTCP(app config.App) (Status, string) {
	conn, err := net.DialTimeout("tcp", app.HealthCheck.Endpoint, c.timeout)
	if err != nil {
		return StatusDown, err.Error()
	}
	conn.Close()
	return StatusUp, ""
}
