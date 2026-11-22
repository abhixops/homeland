package auth

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name, fetchSite, origin, host string
		want                          bool
	}{
		{"same-origin fetch", "same-origin", "", "dash.lan:3000", true},
		{"typed URL", "none", "", "dash.lan:3000", true},
		{"cross-site fetch", "cross-site", "https://evil.example", "dash.lan:3000", false},
		{"sibling subdomain", "same-site", "http://other.lan", "dash.lan:3000", false},
		{"no headers (curl)", "", "", "dash.lan:3000", true},
		{"old browser, matching origin", "", "http://dash.lan:3000", "dash.lan:3000", true},
		{"old browser, other origin", "", "http://evil.example", "dash.lan:3000", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSameOrigin(tt.fetchSite, tt.origin, tt.host); got != tt.want {
				t.Fatalf("isSameOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSameOriginOnlyGuardsWrites(t *testing.T) {
	app := fiber.New()
	app.Use(SameOrigin())
	app.All("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for method, want := range map[string]int{fiber.MethodGet: 200, fiber.MethodPost: 403} {
		req := httptest.NewRequest(method, "/", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Errorf("%s cross-site = %d, want %d", method, resp.StatusCode, want)
		}
	}
}

func TestMiddlewareReadsLiveConfig(t *testing.T) {
	cfg := Config{Enabled: false, Username: "admin", Password: "secret"}
	app := fiber.New()
	app.Use(Middleware(func() Config { return cfg }))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	status := func(user, pass string) int {
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		if user != "" {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	if got := status("", ""); got != 200 {
		t.Fatalf("auth disabled: got %d, want 200", got)
	}
	cfg.Enabled = true // simulate a hot reload turning auth on
	if got := status("", ""); got != 401 {
		t.Fatalf("auth enabled, no creds: got %d, want 401", got)
	}
	if got := status("admin", "secret"); got != 200 {
		t.Fatalf("auth enabled, good creds: got %d, want 200", got)
	}
}
