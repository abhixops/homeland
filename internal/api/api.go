// Package api provides the HTTP handlers for the Homeland REST API and HTML rendering.
package api

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/abhixops/homeland/internal/config"
	dockerpkg "github.com/abhixops/homeland/internal/docker"
	"github.com/abhixops/homeland/internal/health"
	"github.com/abhixops/homeland/internal/icons"
	"github.com/abhixops/homeland/internal/metrics"
	"github.com/gofiber/fiber/v2"
)

// Handler holds references to all services needed by the API.
type Handler struct {
	ConfigMgr      *config.Manager
	HealthChecker  *health.Checker
	IconFetcher    *icons.Fetcher
	DockerDiscover *dockerpkg.Discovery
	MetricsCollect *metrics.Collector
	Templates      *template.Template
	// Version is shown in the footer and at /api/version.
	Version string
	// AssetVersion is appended to CSS/JS URLs to bust stale caches.
	AssetVersion string
}

// calendarDay represents a single day in the calendar grid.
type calendarDay struct {
	Day     int
	IsToday bool
	InMonth bool
}

// NewHandler creates a new API handler and parses all HTML templates.
func NewHandler(
	cfgMgr *config.Manager,
	hc *health.Checker,
	ic *icons.Fetcher,
	dd *dockerpkg.Discovery,
	mc *metrics.Collector,
	templateDir string,
) *Handler {
	// Define template functions available in HTML templates
	funcMap := template.FuncMap{
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"replace":   strings.ReplaceAll,
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mod": func(a, b int) int { return a % b },
		"printf": func(format string, args ...interface{}) string {
			return fmt.Sprintf(format, args...)
		},
		"healthTitle": healthTitle,
		"roundFloat": func(f float64, places int) float64 {
			pow := math.Pow(10, float64(places))
			return math.Round(f*pow) / pow
		},
	}

	// Parse all templates with glob patterns
	tmpl := template.New("").Funcs(funcMap)
	patterns := []string{
		filepath.Join(templateDir, "*.html"),
		filepath.Join(templateDir, "partials", "*.html"),
		filepath.Join(templateDir, "components", "*.html"),
	}
	for _, pattern := range patterns {
		t, err := tmpl.ParseGlob(pattern)
		if err != nil {
			log.Printf("[api] warning: could not parse templates from %s: %v", pattern, err)
		} else {
			tmpl = t
		}
	}

	return &Handler{
		ConfigMgr:      cfgMgr,
		HealthChecker:  hc,
		IconFetcher:    ic,
		DockerDiscover: dd,
		MetricsCollect: mc,
		Templates:      tmpl,
	}
}

// RegisterRoutes sets up all API and page routes on the Fiber app.
func (h *Handler) RegisterRoutes(app *fiber.App) {
	// Page routes (HTMX HTML)
	app.Get("/", h.handleIndex)
	app.Get("/partial/grid", h.handlePartialGrid)
	app.Get("/partial/health", h.handlePartialHealth)
	app.Get("/partial/calendar", h.handlePartialCalendar)
	app.Get("/partial/metrics", h.handlePartialMetrics)
	app.Get("/partial/search", h.handlePartialSearch)
	app.Get("/partial/bookmarks", h.handlePartialBookmarks)

	// REST API routes (JSON)
	api := app.Group("/api")
	api.Get("/apps", h.handleAPIApps)
	api.Get("/health", h.handleAPIHealth)
	api.Get("/health/:app", h.handleAPIHealthApp)
	api.Post("/reload", h.handleAPIReload)
	api.Get("/docker/discover", h.handleAPIDockerDiscover)
	api.Get("/metrics", h.handleAPIMetrics)
	api.Get("/version", h.handleAPIVersion)
}

// ──── Page Handlers ────

func (h *Handler) handleIndex(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)
	healthMap, iconMap := h.appMaps(groups)

	now := time.Now()
	data := fiber.Map{
		"Title":           cfg.Settings.Title,
		"Theme":           cfg.Settings.Theme,
		"Groups":          groups,
		"ShowHistory":     cfg.Settings.HealthCheck.ShowHistory,
		"Summary":         summarize(healthMap),
		"HealthMap":       healthMap,
		"IconMap":         iconMap,
		"Calendar":        h.buildCalendar(now),
		"CurrentDate":     now.Format("January 2, 2006"),
		"MonthYear":       now.Format("January 2006"),
		"Metrics":         h.MetricsCollect.Get(),
		"Year":            now.Year(),
		"Version":         h.Version,
		"AssetVersion":    h.AssetVersion,
		"Bookmarks":       cfg.Bookmarks,
		"BookmarkIconMap": h.bookmarkIcons(cfg),
	}

	// Always revalidate the page so it picks up new asset URLs after an upgrade.
	c.Set(fiber.HeaderCacheControl, "no-cache")
	return h.renderTemplate(c, "index.html", data)
}

func (h *Handler) handlePartialGrid(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)
	healthMap, iconMap := h.appMaps(groups)
	data := fiber.Map{
		"Groups":      groups,
		"HealthMap":   healthMap,
		"IconMap":     iconMap,
		"ShowHistory": cfg.Settings.HealthCheck.ShowHistory,
		"Summary":     summarize(healthMap),
	}
	return h.renderTemplate(c, "app_grid.html", data)
}

func (h *Handler) handlePartialHealth(c *fiber.Ctx) error {
	healthMap, _ := h.appMaps(h.getMergedGroups(h.ConfigMgr.Get()))
	return h.renderTemplate(c, "health_dot.html", fiber.Map{"HealthMap": healthMap})
}

func (h *Handler) handlePartialCalendar(c *fiber.Ctx) error {
	now := time.Now()
	data := fiber.Map{
		"Calendar":  h.buildCalendar(now),
		"MonthYear": now.Format("January 2006"),
	}
	return h.renderTemplate(c, "calendar.html", data)
}

func (h *Handler) handlePartialMetrics(c *fiber.Ctx) error {
	return h.renderTemplate(c, "metrics.html", fiber.Map{"Metrics": h.MetricsCollect.Get()})
}

func (h *Handler) handlePartialBookmarks(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	data := fiber.Map{
		"Bookmarks":       cfg.Bookmarks,
		"BookmarkIconMap": h.bookmarkIcons(cfg),
	}
	return h.renderTemplate(c, "bookmarks.html", data)
}

func (h *Handler) handlePartialSearch(c *fiber.Ctx) error {
	query := strings.ToLower(c.Query("q", ""))
	groups := h.getMergedGroups(h.ConfigMgr.Get())

	var results []config.App
	if query != "" {
		for _, g := range groups {
			for _, app := range g.Apps {
				if fuzzyMatch(strings.ToLower(app.Name), query) ||
					fuzzyMatch(strings.ToLower(app.Description), query) {
					results = append(results, app)
				}
			}
		}
	}
	healthMap, iconMap := h.appMaps([]config.Group{{Apps: results}})

	data := fiber.Map{
		"Results":   results,
		"Query":     query,
		"IconMap":   iconMap,
		"HealthMap": healthMap,
	}
	return h.renderTemplate(c, "search.html", data)
}

// ──── REST API Handlers ────

func (h *Handler) handleAPIApps(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	return c.JSON(fiber.Map{"groups": h.getMergedGroups(cfg)})
}

func (h *Handler) handleAPIHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"health": h.HealthChecker.GetAll()})
}

func (h *Handler) handleAPIHealthApp(c *fiber.Ctx) error {
	return c.JSON(h.HealthChecker.GetStatus(c.Params("app")))
}

// handleAPIReload re-reads the config file from disk. Change callbacks
// registered on the config manager restart health checks and so on.
func (h *Handler) handleAPIReload(c *fiber.Ctx) error {
	if err := h.ConfigMgr.Reload(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "reloaded"})
}

func (h *Handler) handleAPIDockerDiscover(c *fiber.Ctx) error {
	if h.DockerDiscover == nil {
		return c.JSON(fiber.Map{"discovered": []config.App{}, "count": 0})
	}
	h.DockerDiscover.Discover()
	apps := h.DockerDiscover.GetApps()
	return c.JSON(fiber.Map{"discovered": apps, "count": len(apps)})
}

func (h *Handler) handleAPIMetrics(c *fiber.Ctx) error {
	return c.JSON(h.MetricsCollect.Get())
}

func (h *Handler) handleAPIVersion(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"version": h.Version})
}

// ──── Helpers ────

// getMergedGroups combines YAML groups with Docker-discovered groups.
func (h *Handler) getMergedGroups(cfg *config.Config) []config.Group {
	if h.DockerDiscover != nil && h.DockerDiscover.IsAvailable() {
		return dockerpkg.MergeWithConfig(cfg.Groups, h.DockerDiscover.GetGroups())
	}
	return cfg.Groups
}

// appMaps returns the health result and icon URL for every app in groups,
// keyed by app name.
func (h *Handler) appMaps(groups []config.Group) (map[string]health.Result, map[string]string) {
	healthMap := make(map[string]health.Result)
	iconMap := make(map[string]string)
	for _, g := range groups {
		for _, app := range g.Apps {
			healthMap[app.Name] = h.HealthChecker.GetStatus(app.Name)
			iconMap[app.Name] = h.IconFetcher.GetIconURL(app.Icon)
		}
	}
	return healthMap, iconMap
}

// healthSummary counts apps by status for the "X of Y online" line.
type healthSummary struct {
	Up, Down, Checked int
}

func summarize(healthMap map[string]health.Result) healthSummary {
	var s healthSummary
	for _, r := range healthMap {
		switch r.Status {
		case health.StatusUp:
			s.Up++
		case health.StatusDown:
			s.Down++
		}
	}
	s.Checked = s.Up + s.Down
	return s
}

// bookmarkIcons returns the icon URL for each bookmark that names an icon.
func (h *Handler) bookmarkIcons(cfg *config.Config) map[string]string {
	icons := make(map[string]string)
	for _, bg := range cfg.Bookmarks {
		for _, bm := range bg.Bookmarks {
			if bm.Icon != "" {
				icons[bm.Name] = h.IconFetcher.GetIconURL(bm.Icon)
			}
		}
	}
	return icons
}

// healthTitle is the hover text for an app's status dot, e.g.
// "Online · 42 ms · 97% of last 30 checks".
func healthTitle(r health.Result) string {
	var parts []string
	switch r.Status {
	case health.StatusUp:
		parts = append(parts, "Online", fmt.Sprintf("%d ms", r.Latency))
	case health.StatusDown:
		parts = append(parts, "Offline")
		if r.Error != "" {
			parts = append(parts, r.Error)
		}
	default:
		parts = append(parts, "Status unknown")
	}
	if pct := r.UptimePercent(); pct >= 0 {
		parts = append(parts, fmt.Sprintf("%.0f%% up over last %d checks", pct, len(r.History)))
	}
	return strings.Join(parts, " · ")
}

// buildCalendar generates the calendar grid for the given month.
func (h *Handler) buildCalendar(now time.Time) [][]calendarDay {
	year, month, today := now.Year(), now.Month(), now.Day()
	firstDay := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
	startWeekday := int(firstDay.Weekday()) // 0=Sunday
	lastDay := firstDay.AddDate(0, 1, -1)
	daysInMonth := lastDay.Day()

	var weeks [][]calendarDay
	day := 1 - startWeekday
	for week := 0; week < 6; week++ {
		var weekDays []calendarDay
		for dow := 0; dow < 7; dow++ {
			d := calendarDay{}
			if day >= 1 && day <= daysInMonth {
				d.Day = day
				d.InMonth = true
				d.IsToday = day == today
			} else if day < 1 {
				prevMonth := firstDay.AddDate(0, 0, day-1)
				d.Day = prevMonth.Day()
			} else {
				d.Day = day - daysInMonth
			}
			weekDays = append(weekDays, d)
			day++
		}
		weeks = append(weeks, weekDays)
		if day > daysInMonth {
			break
		}
	}
	return weeks
}

// fuzzyMatch checks if the query appears as a subsequence or substring of the text.
func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(text, query) {
		return true
	}
	qi := 0
	for i := 0; i < len(text) && qi < len(query); i++ {
		if text[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// renderTemplate executes a named template safely. If execution fails,
// it returns a minimal HTML fragment to prevent HTMX from thrashing the DOM.
func (h *Handler) renderTemplate(c *fiber.Ctx, name string, data interface{}) error {
	var buf bytes.Buffer
	if err := h.Templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("[api] template %s error: %v", name, err)
		// Return a safe empty fragment — HTMX will swap this in without breaking anything
		c.Set("Content-Type", "text/html")
		return c.Status(200).SendString("<!-- template render error, retrying next cycle -->")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}
