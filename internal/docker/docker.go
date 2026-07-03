// Package docker provides auto-discovery of services via Docker container labels.
package docker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/abhixops/homeland/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Label constants for Docker service discovery.
const (
	LabelName  = "homepage.name"
	LabelIcon  = "homepage.icon"
	LabelURL   = "homepage.url"
	LabelGroup = "homepage.group"
	LabelDesc  = "homepage.description"
)

// Discovery manages Docker container discovery and merging with YAML config.
type Discovery struct {
	client     *client.Client
	cancel     context.CancelFunc
	groups     map[string][]config.App // group name → apps
	apps       []config.App
	mu         sync.RWMutex
	refreshInt time.Duration
	available  bool
}

// NewDiscovery creates a new Docker discovery instance.
// It attempts to connect to Docker immediately, but if the daemon is not yet
// available (common after system restarts), it will retry automatically on
// each discovery tick rather than permanently failing.
func NewDiscovery(refreshInterval time.Duration) *Discovery {
	d := &Discovery{
		groups:     make(map[string][]config.App),
		refreshInt: refreshInterval,
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("[docker] Docker client unavailable: %v", err)
		// No client at all — cannot retry without a client handle.
		d.available = false
		return d
	}

	// Store the client regardless of Ping outcome so we can retry later.
	d.client = cli

	// Attempt initial connection (non-blocking on failure).
	if !d.tryConnect() {
		log.Println("[docker] Docker daemon not reachable at startup, will retry in background")
	}

	return d
}

// tryConnect attempts to ping the Docker daemon. Returns true on success.
// Safe to call repeatedly; it is a no-op if already connected.
func (d *Discovery) tryConnect() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.available {
		return true
	}
	if d.client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := d.client.Ping(ctx); err != nil {
		log.Printf("[docker] Docker daemon not reachable: %v", err)
		return false
	}

	d.available = true
	log.Println("[docker] connected to Docker daemon")
	return true
}

// IsAvailable returns true if Docker is reachable.
func (d *Discovery) IsAvailable() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.available
}

// Start begins periodic container discovery in a background goroutine.
// If Docker is not yet available, the ticker loop will retry the connection
// on each cycle until it succeeds.
func (d *Discovery) Start() {
	// If we don't even have a client handle, there's nothing to retry.
	if d.client == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// Discover immediately (if connected)
	if d.IsAvailable() {
		d.Discover()
	}

	go func() {
		ticker := time.NewTicker(d.refreshInt)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// If not yet connected, attempt reconnection before discovering.
				if !d.IsAvailable() {
					d.tryConnect()
				}
				if d.IsAvailable() {
					d.Discover()
				}
			}
		}
	}()
}

// Stop cancels the discovery goroutine.
func (d *Discovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

// Discover queries Docker for running containers with homepage labels.
func (d *Discovery) Discover() {
	if !d.IsAvailable() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	containers, err := d.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("[docker] failed to list containers: %v", err)
		return
	}

	groups := make(map[string][]config.App)
	var apps []config.App

	for _, c := range containers {
		name, ok := c.Labels[LabelName]
		if !ok {
			continue // Skip containers without homepage labels
		}

		app := config.App{
			Name:   name,
			URL:    c.Labels[LabelURL],
			Icon:   c.Labels[LabelIcon],
			Source: "docker",
		}
		if desc, ok := c.Labels[LabelDesc]; ok {
			app.Description = desc
		}

		// If a URL health endpoint is set, create a basic HTTP health check
		if app.URL != "" {
			app.HealthCheck = &config.HealthCheck{
				Type:     "http",
				Endpoint: "/",
			}
		}

		groupName := c.Labels[LabelGroup]
		if groupName == "" {
			groupName = "Docker"
		}

		groups[groupName] = append(groups[groupName], app)
		apps = append(apps, app)
	}

	d.mu.Lock()
	d.apps = apps
	d.groups = groups
	d.mu.Unlock()

	log.Printf("[docker] discovered %d apps in %d groups", len(apps), len(groups))
}

// GetApps returns all discovered Docker apps.
func (d *Discovery) GetApps() []config.App {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]config.App, len(d.apps))
	copy(result, d.apps)
	return result
}

// GetGroups returns discovered apps organized by group.
func (d *Discovery) GetGroups() map[string][]config.App {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string][]config.App)
	for k, v := range d.groups {
		apps := make([]config.App, len(v))
		copy(apps, v)
		result[k] = apps
	}
	return result
}

// ContainerCount returns the number of running containers.
// If Docker was previously unreachable, it attempts a reconnection first.
func (d *Discovery) ContainerCount() int {
	if !d.IsAvailable() {
		// Attempt lazy reconnection so metrics recover without waiting
		// for the next discovery tick.
		if !d.tryConnect() {
			return 0
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	containers, err := d.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("[docker] failed to count containers: %v", err)
		return 0
	}
	return len(containers)
}

// MergeWithConfig combines YAML-defined groups with Docker-discovered groups.
// YAML apps take precedence: if an app with the same name exists in both, the YAML version wins.
func MergeWithConfig(yamlGroups []config.Group, dockerGroups map[string][]config.App) []config.Group {
	// Build a set of YAML app names for deduplication
	yamlApps := make(map[string]bool)
	for _, g := range yamlGroups {
		for _, a := range g.Apps {
			yamlApps[a.Name] = true
		}
	}

	// Index YAML groups by name for merging
	groupIndex := make(map[string]int)
	result := make([]config.Group, len(yamlGroups))
	copy(result, yamlGroups)
	for i, g := range result {
		groupIndex[g.Name] = i
	}

	// Merge Docker apps into existing or new groups
	for groupName, apps := range dockerGroups {
		var newApps []config.App
		for _, app := range apps {
			if !yamlApps[app.Name] {
				newApps = append(newApps, app)
			}
		}
		if len(newApps) == 0 {
			continue
		}

		if idx, exists := groupIndex[groupName]; exists {
			result[idx].Apps = append(result[idx].Apps, newApps...)
		} else {
			result = append(result, config.Group{
				Name: groupName,
				Icon: "docker",
				Apps: newApps,
			})
		}
	}

	return result
}
