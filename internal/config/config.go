// Package config handles YAML configuration loading and hot-reload via fsnotify.
package config

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure parsed from homepage.yaml.
type Config struct {
	Settings Settings `yaml:"settings"`
	Groups   []Group  `yaml:"groups"`
}

// Settings holds global application settings.
type Settings struct {
	Title       string      `yaml:"title"`
	Port        int         `yaml:"port"`
	Theme       string      `yaml:"theme"`
	HealthCheck HealthCfg   `yaml:"health_check"`
	Auth        AuthCfg     `yaml:"auth"`
}

// HealthCfg holds default health check parameters.
type HealthCfg struct {
	Interval int `yaml:"interval"` // seconds
	Timeout  int `yaml:"timeout"`  // seconds
}

// AuthCfg holds basic authentication settings.
type AuthCfg struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Group represents a named group of applications.
type Group struct {
	Name string `yaml:"name"`
	Icon string `yaml:"icon"`
	Apps []App  `yaml:"apps"`
}

// App represents a single application entry.
type App struct {
	Name        string       `yaml:"name"`
	URL         string       `yaml:"url"`
	Icon        string       `yaml:"icon"`
	Description string       `yaml:"description"`
	HealthCheck *HealthCheck `yaml:"healthcheck,omitempty"`
	// Source tracks where this app was discovered ("yaml" or "docker")
	Source string `yaml:"-"`
}

// HealthCheck defines how to check if an app is reachable.
type HealthCheck struct {
	Type     string `yaml:"type"`     // "http" or "tcp"
	Endpoint string `yaml:"endpoint"` // path for HTTP, host:port for TCP
}

// Manager provides thread-safe access to the loaded configuration
// and supports hot-reloading the config file on changes.
type Manager struct {
	mu       sync.RWMutex
	config   *Config
	filePath string
	onChange []func(*Config) // callbacks invoked after reload
	watcher  *fsnotify.Watcher
	stopOnce sync.Once
}

// Load reads and parses the YAML config file at path, returning the
// parsed *Config or a wrapped error. This is the standalone entry point;
// it does not require a Manager.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	applyDefaults(&cfg)
	markYAMLSource(&cfg)
	return &cfg, nil
}

// NewManager loads the config from the given YAML file and returns a Manager.
func NewManager(filePath string) (*Manager, error) {
	m := &Manager{filePath: filePath}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Get returns the current configuration (read-locked).
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// OnChange registers a callback that fires after a successful config reload.
func (m *Manager) OnChange(fn func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = append(m.onChange, fn)
}

// Reload re-reads the config file from disk. If the file is valid the
// stored config is updated; otherwise the old config is kept and the
// error is returned.
func (m *Manager) Reload() error {
	cfg, err := Load(m.filePath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
	return nil
}

// Watch starts an fsnotify watcher on the config file. When the file is
// written or re-created the config is reloaded and onChange is called
// with the new *Config. If the new file is invalid the old config is
// kept and onChange is not called. Includes a 500ms debounce to handle
// spurious events from Docker bind-mounts or atomic saves.
func (m *Manager) Watch(onChange func(*Config)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("config: create watcher: %w", err)
	}
	if err := w.Add(m.filePath); err != nil {
		w.Close()
		return fmt.Errorf("config: watch %s: %w", m.filePath, err)
	}
	m.watcher = w
	m.stopOnce = sync.Once{} // reset for a fresh watcher

	go func() {
		var debounce *time.Timer
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					// Debounce: reset timer on each event, only fire after 500ms of quiet
					if debounce != nil {
						debounce.Stop()
					}
					debounce = time.AfterFunc(500*time.Millisecond, func() {
						log.Printf("[config] detected change in %s, reloading...", m.filePath)
						cfg, err := Load(m.filePath)
						if err != nil {
							log.Printf("[config] reload error: %v", err)
							return
						}
						m.mu.Lock()
						m.config = cfg
						m.mu.Unlock()
						if onChange != nil {
							onChange(cfg)
						}
					})
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("[config] watcher error: %v", err)
			}
		}
	}()
	return nil
}

// StopWatch closes the fsnotify watcher, causing the Watch goroutine
// to exit. It is safe to call multiple times.
func (m *Manager) StopWatch() {
	m.stopOnce.Do(func() {
		if m.watcher != nil {
			m.watcher.Close()
		}
	})
}

// load reads and parses the YAML config file (internal helper).
func (m *Manager) load() error {
	cfg, err := Load(m.filePath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
	return nil
}

// applyDefaults fills in zero-valued settings with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Settings.Port == 0 {
		cfg.Settings.Port = 3000
	}
	if cfg.Settings.Title == "" {
		cfg.Settings.Title = "Homeland"
	}
	if cfg.Settings.Theme == "" {
		cfg.Settings.Theme = "dark"
	}
	if cfg.Settings.HealthCheck.Interval == 0 {
		cfg.Settings.HealthCheck.Interval = 30
	}
	if cfg.Settings.HealthCheck.Timeout == 0 {
		cfg.Settings.HealthCheck.Timeout = 5
	}
}

// markYAMLSource sets Source="yaml" on every app.
func markYAMLSource(cfg *Config) {
	for i := range cfg.Groups {
		for j := range cfg.Groups[i].Apps {
			cfg.Groups[i].Apps[j].Source = "yaml"
		}
	}
}
