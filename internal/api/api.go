// Package api provides the HTTP handlers for the Homeland REST API and HTML rendering.
package api

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"math"
	"os"
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
	Compose        *composeController
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
		Compose:        newComposeController(os.Getenv("HOMELAND_SOURCES_DIR")),
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
	api.Get("/compose/projects", h.handleAPIComposeProjects)
	api.Post("/compose", h.handleAPICompose)
}

// ──── Page Handlers ────

func (h *Handler) handleIndex(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)

	// Build health status map for all apps
	healthMap := make(map[string]string)
	iconMap := make(map[string]string)
	for _, g := range groups {
		for _, app := range g.Apps {
			result := h.HealthChecker.GetStatus(app.Name)
			healthMap[app.Name] = string(result.Status)
			iconMap[app.Name] = h.IconFetcher.GetIconURL(app.Icon)
		}
	}

	// Build bookmark icon map
	bookmarkIconMap := make(map[string]string)
	for _, bg := range cfg.Bookmarks {
		for _, bm := range bg.Bookmarks {
			if bm.Icon != "" {
				bookmarkIconMap[bm.Name] = h.IconFetcher.GetIconURL(bm.Icon)
			}
		}
	}

	now := time.Now()
	data := fiber.Map{
		"Title":           cfg.Settings.Title,
		"Theme":           cfg.Settings.Theme,
		"Groups":          groups,
		"HealthMap":       healthMap,
		"IconMap":         iconMap,
		"Calendar":        h.buildCalendar(now),
		"CurrentDate":     now.Format("January 2, 2006"),
		"MonthYear":       now.Format("January 2006"),
		"Metrics":         h.MetricsCollect.Get(),
		"Year":            now.Year(),
		"Bookmarks":       cfg.Bookmarks,
		"BookmarkIconMap": bookmarkIconMap,
	}

	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "index.html", data)
}

func (h *Handler) handlePartialGrid(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)

	healthMap := make(map[string]string)
	iconMap := make(map[string]string)
	for _, g := range groups {
		for _, app := range g.Apps {
			result := h.HealthChecker.GetStatus(app.Name)
			healthMap[app.Name] = string(result.Status)
			iconMap[app.Name] = h.IconFetcher.GetIconURL(app.Icon)
		}
	}

	data := fiber.Map{"Groups": groups, "HealthMap": healthMap, "IconMap": iconMap}
	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "app_grid.html", data)
}

func (h *Handler) handlePartialHealth(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)

	healthMap := make(map[string]string)
	for _, g := range groups {
		for _, app := range g.Apps {
			result := h.HealthChecker.GetStatus(app.Name)
			healthMap[app.Name] = string(result.Status)
		}
	}

	data := fiber.Map{"HealthMap": healthMap}
	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "health_dot.html", data)
}

func (h *Handler) handlePartialCalendar(c *fiber.Ctx) error {
	now := time.Now()
	data := fiber.Map{
		"Calendar":  h.buildCalendar(now),
		"MonthYear": now.Format("January 2006"),
	}
	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "calendar.html", data)
}

func (h *Handler) handlePartialMetrics(c *fiber.Ctx) error {
	data := fiber.Map{"Metrics": h.MetricsCollect.Get()}
	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "metrics.html", data)
}

func (h *Handler) handlePartialBookmarks(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	bookmarkIconMap := make(map[string]string)
	for _, bg := range cfg.Bookmarks {
		for _, bm := range bg.Bookmarks {
			if bm.Icon != "" {
				bookmarkIconMap[bm.Name] = h.IconFetcher.GetIconURL(bm.Icon)
			}
		}
	}
	data := fiber.Map{
		"Bookmarks":       cfg.Bookmarks,
		"BookmarkIconMap": bookmarkIconMap,
	}
	c.Set("Content-Type", "text/html")
	return h.renderTemplate(c, "bookmarks.html", data)
}

func (h *Handler) handlePartialSearch(c *fiber.Ctx) error {
	query := strings.ToLower(c.Query("q", ""))
	cfg := h.ConfigMgr.Get()
	groups := h.getMergedGroups(cfg)

	var results []config.App
	iconMap := make(map[string]string)
	healthMap := make(map[string]string)

	if query != "" {
		for _, g := range groups {
			for _, app := range g.Apps {
				if fuzzyMatch(strings.ToLower(app.Name), query) ||
					fuzzyMatch(strings.ToLower(app.Description), query) {
					results = append(results, app)
					iconMap[app.Name] = h.IconFetcher.GetIconURL(app.Icon)
					result := h.HealthChecker.GetStatus(app.Name)
					healthMap[app.Name] = string(result.Status)
				}
			}
		}
	}

	data := fiber.Map{
		"Results":   results,
		"Query":     query,
		"IconMap":   iconMap,
		"HealthMap": healthMap,
	}
	c.Set("Content-Type", "text/html")
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

func (h *Handler) handleAPIReload(c *fiber.Ctx) error {
	cfg := h.ConfigMgr.Get()
	h.HealthChecker.Start(cfg)
	return c.JSON(fiber.Map{"status": "reloaded"})
}

func (h *Handler) handleAPIDockerDiscover(c *fiber.Ctx) error {
	h.DockerDiscover.Discover()
	apps := h.DockerDiscover.GetApps()
	return c.JSON(fiber.Map{"discovered": apps, "count": len(apps)})
}

func (h *Handler) handleAPIMetrics(c *fiber.Ctx) error {
	return c.JSON(h.MetricsCollect.Get())
}

func (h *Handler) handleAPIComposeProjects(c *fiber.Ctx) error {
	projects, err := h.Compose.projects()
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"projects": projects})
}

func (h *Handler) handleAPICompose(c *fiber.Ctx) error {
	var request struct {
		Project string `json:"project"`
		Action  string `json:"action"`
	}
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	output, err := h.Compose.run(c.Context(), request.Project, request.Action)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "output": output})
	}
	return c.JSON(fiber.Map{"status": "ok", "project": request.Project, "action": request.Action, "output": output})
}

// ──── Helpers ────

// getMergedGroups combines YAML groups with Docker-discovered groups.
func (h *Handler) getMergedGroups(cfg *config.Config) []config.Group {
	if h.DockerDiscover != nil && h.DockerDiscover.IsAvailable() {
		return dockerpkg.MergeWithConfig(cfg.Groups, h.DockerDiscover.GetGroups())
	}
	return cfg.Groups
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
	return c.Send(buf.Bytes())
}
