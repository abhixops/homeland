package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validYAML = `
settings:
  title: "TestApp"
  port: 8080
  theme: light
  health_check:
    interval: 10
    timeout: 2
  auth:
    enabled: true
    username: admin
    password: secret

groups:
  - name: TestGroup
    icon: star
    apps:
      - name: App1
        url: http://localhost:1000
        icon: app1
        description: First app
`

const updatedYAML = `
settings:
  title: "UpdatedApp"
  port: 9090
  theme: dark

groups:
  - name: NewGroup
    icon: bolt
    apps:
      - name: App2
        url: http://localhost:2000
        icon: app2
        description: Second app
`

const invalidYAML = `
settings:
  title: [[[invalid
`

// helper: write content to a temp YAML file and return its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ─── Load ────────────────────────────────────────────────────────

func TestLoad_Valid(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Settings.Title != "TestApp" {
		t.Errorf("title = %q; want TestApp", cfg.Settings.Title)
	}
	if cfg.Settings.Port != 8080 {
		t.Errorf("port = %d; want 8080", cfg.Settings.Port)
	}
	if len(cfg.Groups) != 1 {
		t.Fatalf("groups len = %d; want 1", len(cfg.Groups))
	}
	if cfg.Groups[0].Apps[0].Source != "yaml" {
		t.Errorf("source = %q; want yaml", cfg.Groups[0].Apps[0].Source)
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Minimal YAML with no explicit settings — all defaults should apply.
	p := writeTempConfig(t, "groups: []\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Settings.Port != 3000 {
		t.Errorf("default port = %d; want 3000", cfg.Settings.Port)
	}
	if cfg.Settings.Title != "Homeland" {
		t.Errorf("default title = %q; want Homeland", cfg.Settings.Title)
	}
	if cfg.Settings.Theme != "dark" {
		t.Errorf("default theme = %q; want dark", cfg.Settings.Theme)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/tmp/nonexistent_config_test.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	p := writeTempConfig(t, invalidYAML)
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// ─── NewManager + Get ────────────────────────────────────────────

func TestNewManager(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	cfg := mgr.Get()
	if cfg.Settings.Title != "TestApp" {
		t.Errorf("title = %q; want TestApp", cfg.Settings.Title)
	}
}

func TestNewManager_InvalidFile(t *testing.T) {
	_, err := NewManager("/tmp/nonexistent_config_test.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ─── Reload ──────────────────────────────────────────────────────

func TestReload(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite the file with updated content.
	if err := os.WriteFile(p, []byte(updatedYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	cfg := mgr.Get()
	if cfg.Settings.Title != "UpdatedApp" {
		t.Errorf("title after reload = %q; want UpdatedApp", cfg.Settings.Title)
	}
}

func TestReload_InvalidKeepsOld(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite with invalid YAML.
	if err := os.WriteFile(p, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reload(); err == nil {
		t.Fatal("expected error from Reload with invalid YAML")
	}
	// Old config should be preserved.
	cfg := mgr.Get()
	if cfg.Settings.Title != "TestApp" {
		t.Errorf("title should remain TestApp; got %q", cfg.Settings.Title)
	}
}

// ─── Watch + StopWatch ───────────────────────────────────────────

func TestWatch_CallsOnChange(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatal(err)
	}

	changed := make(chan *Config, 1)
	if err := mgr.Watch(func(cfg *Config) {
		changed <- cfg
	}); err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	defer mgr.StopWatch()

	// Give the watcher time to start.
	time.Sleep(100 * time.Millisecond)

	// Update the file.
	if err := os.WriteFile(p, []byte(updatedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-changed:
		if cfg.Settings.Title != "UpdatedApp" {
			t.Errorf("onChange title = %q; want UpdatedApp", cfg.Settings.Title)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for onChange callback")
	}
}

func TestWatch_InvalidYAMLKeepsOld(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatal(err)
	}

	called := make(chan struct{}, 1)
	if err := mgr.Watch(func(cfg *Config) {
		called <- struct{}{}
	}); err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	defer mgr.StopWatch()

	time.Sleep(100 * time.Millisecond)

	// Write invalid YAML — onChange must NOT fire.
	if err := os.WriteFile(p, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-called:
		t.Fatal("onChange should not have been called for invalid YAML")
	case <-time.After(2 * time.Second):
		// good — callback was not invoked
	}

	// Config should still be the original.
	cfg := mgr.Get()
	if cfg.Settings.Title != "TestApp" {
		t.Errorf("title should remain TestApp; got %q", cfg.Settings.Title)
	}
}

func TestStopWatch(t *testing.T) {
	p := writeTempConfig(t, validYAML)
	mgr, err := NewManager(p)
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.Watch(func(cfg *Config) {}); err != nil {
		t.Fatalf("Watch error: %v", err)
	}

	// StopWatch should not panic even when called twice.
	mgr.StopWatch()
	mgr.StopWatch()
}
