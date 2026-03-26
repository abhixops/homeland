// Package docker provides auto-discovery of services via Docker container labels.
package docker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/abhishek/homeland/internal/config"
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
	mu         sync.RWMutex
	apps       []config.App
	groups     map[string][]config.App // group name → apps
	client     *client.Client
	available  bool
	refreshInt time.Duration
	cancel     context.CancelFunc
}

// NewDiscovery creates a new Docker discovery instance.
// It gracefully handles the Docker socket being unavailable.
func NewDiscovery(refreshInterval time.Duration) *Discovery {
	d := &Discovery{
		groups:     make(map[string][]config.App),
		refreshInt: refreshInterval,
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("[docker] Docker client unavailable: %v", err)
		d.available = false
		return d
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		log.Printf("[docker] Docker daemon not reachable: %v", err)
		d.available = false
		return d
	}

	d.client = cli
	d.available = true
	log.Println("[docker] connected to Docker daemon")
	return d
}

// IsAvailable returns true if Docker is reachable.
func (d *Discovery) IsAvailable() bool {
	return d.available
}

// Start begins periodic container discovery in a background goroutine.
func (d *Discovery) Start() {
	if !d.available {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	// Discover immediately
	d.Discover()

	go func() {
		ticker := time.NewTicker(d.refreshInt)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.Discover()
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
	if !d.available {
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
func (d *Discovery) ContainerCount() int {
	if !d.available {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	containers, err := d.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
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
