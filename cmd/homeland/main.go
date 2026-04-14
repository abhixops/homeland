// Homeland — A lightweight, self-hosted homepage dashboard for homelab environments.
//
// This is the main entry point that wires together all subsystems:
// config loader, health checker, Docker discovery, icon fetcher, metrics, and the API server.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/abhixops/homeland/internal/api"
	"github.com/abhixops/homeland/internal/auth"
	"github.com/abhixops/homeland/internal/config"
	"github.com/abhixops/homeland/internal/docker"
	"github.com/abhixops/homeland/internal/health"
	"github.com/abhixops/homeland/internal/icons"
	"github.com/abhixops/homeland/internal/metrics"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	start := time.Now()
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("🏠 Homeland Dashboard starting...")

	// ── Determine paths ──
	// Look for config relative to the binary or in configs/ directory
	configPath := getConfigPath()
	baseDir := getBaseDir()
	templateDir := filepath.Join(baseDir, "web", "templates")
	staticDir := filepath.Join(baseDir, "web", "static")
	iconCacheDir := filepath.Join(staticDir, "icons")

	// ── Load Configuration ──
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		log.Fatalf("failed to load config from %s: %v", configPath, err)
	}
	cfg := cfgMgr.Get()
	log.Printf("[config] loaded %d groups from %s", len(cfg.Groups), configPath)

	// Start watching for config changes (hot-reload)
	cfgMgr.WatchFile()

	// ── Initialize Subsystems ──

	// Health checker — runs periodic HTTP/TCP checks in goroutines
	healthChecker := health.NewChecker(
		cfg.Settings.HealthCheck.Interval,
		cfg.Settings.HealthCheck.Timeout,
	)
	healthChecker.Start(cfg)

	// Re-wire health checks when config changes
	cfgMgr.OnChange(func(newCfg *config.Config) {
		healthChecker.Start(newCfg)
	})

	// Icon fetcher — downloads and caches icons from selfh.st CDN
	iconFetcher := icons.NewFetcher(iconCacheDir)
	iconStop := make(chan struct{})

	// Preload icons for all configured apps in parallel
	var iconNames []string
	for _, g := range cfg.Groups {
		for _, app := range g.Apps {
			iconNames = append(iconNames, app.Icon)
		}
	}
	iconFetcher.PreloadIcons(iconNames)

	// Start background retry loop for any icons that failed during preload
	iconFetcher.StartRetryLoop(iconStop)

	// Docker discovery — reads container labels for auto-discovery
	dockerDiscovery := docker.NewDiscovery(60 * time.Second)
	dockerDiscovery.Start()

	// System metrics collector — CPU, memory, uptime, container count
	metricsCollector := metrics.NewCollector(10, dockerDiscovery)
	metricsCollector.Start()

	// ── Create Fiber App ──
	app := fiber.New(fiber.Config{
		AppName:               "Homeland",
		DisableStartupMessage: false,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          10 * time.Second,
		IdleTimeout:           30 * time.Second,
		EnableTrustedProxyCheck: false,
		ProxyHeader:           fiber.HeaderXForwardedFor,
	})

	// Global middleware
	app.Use(recover.New())  // Recover from panics
	app.Use(compress.New()) // Gzip compression
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: "15:04:05",
	}))

	// Optional basic auth
	authCfg := &auth.Config{
		Enabled:  cfg.Settings.Auth.Enabled,
		Username: cfg.Settings.Auth.Username,
		Password: cfg.Settings.Auth.Password,
	}
	app.Use(auth.Middleware(authCfg))

	// Serve static files (CSS, JS, icons)
	app.Static("/static", staticDir, fiber.Static{
		Compress:      true,
		CacheDuration: 24 * time.Hour,
	})

	// ── Register Routes ──
	handler := api.NewHandler(cfgMgr, healthChecker, iconFetcher, dockerDiscovery, metricsCollector, templateDir)
	handler.RegisterRoutes(app)

	// ── Graceful Shutdown ──
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		log.Println("shutting down...")
		close(iconStop)
		healthChecker.Stop()
		dockerDiscovery.Stop()
		metricsCollector.Stop()
		app.Shutdown()
	}()

	// ── Start Server ──
	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Settings.Port)
	log.Printf("🚀 Homeland ready in %v — listening on %s", time.Since(start).Round(time.Millisecond), addr)

	if err := app.Listen(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// getConfigPath resolves the config file path.
// Priority: HOMELAND_CONFIG env var → ./configs/homepage.yaml → /etc/homeland/homepage.yaml
func getConfigPath() string {
	if envPath := os.Getenv("HOMELAND_CONFIG"); envPath != "" {
		return envPath
	}
	candidates := []string{
		"configs/homepage.yaml",
		"/etc/homeland/homepage.yaml",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "configs/homepage.yaml" // default, will error on load if missing
}

// getBaseDir returns the base directory for templates and static files.
// Priority: HOMELAND_BASE env var → current working directory
func getBaseDir() string {
	if envBase := os.Getenv("HOMELAND_BASE"); envBase != "" {
		return envBase
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
