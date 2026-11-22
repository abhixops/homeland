// Package auth provides optional Basic HTTP authentication middleware for Fiber.
package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Config holds the authentication configuration.
type Config struct {
	Username string
	Password string
	Enabled  bool
}

// Middleware returns a Fiber middleware that enforces HTTP Basic Auth
// when authentication is enabled. If disabled, it passes through.
// getConfig is called on every request, so auth changes apply on hot reload.
func Middleware(getConfig func() Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		cfg := getConfig()
		if !cfg.Enabled {
			return c.Next()
		}

		// Skip auth for static assets (icons, CSS, JS)
		path := c.Path()
		if strings.HasPrefix(path, "/static/") {
			return c.Next()
		}

		auth := c.Get("Authorization")
		if auth == "" {
			return unauthorized(c)
		}

		// Parse "Basic <base64>"
		if !strings.HasPrefix(auth, "Basic ") {
			return unauthorized(c)
		}

		decoded, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			return unauthorized(c)
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			return unauthorized(c)
		}

		// Constant-time comparison to prevent timing attacks
		usernameMatch := subtle.ConstantTimeCompare([]byte(parts[0]), []byte(cfg.Username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(parts[1]), []byte(cfg.Password)) == 1

		if !usernameMatch || !passwordMatch {
			return unauthorized(c)
		}

		return c.Next()
	}
}

// SameOrigin returns a middleware that rejects state-changing requests
// (anything but GET, HEAD and OPTIONS) sent by another website through the
// visitor's browser (cross-site request forgery). Non-browser clients such as
// curl send neither header and are allowed.
func SameOrigin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}
		if !isSameOrigin(c.Get("Sec-Fetch-Site"), c.Get(fiber.HeaderOrigin), string(c.Request().Host())) {
			return c.Status(fiber.StatusForbidden).SendString("Cross-site request blocked")
		}
		return c.Next()
	}
}

// isSameOrigin decides whether a request came from this dashboard's own pages.
// Modern browsers send Sec-Fetch-Site; older ones fall back to the Origin header.
func isSameOrigin(fetchSite, origin, host string) bool {
	switch fetchSite {
	case "same-origin", "none":
		return true
	case "":
		// Header not sent: old browser or a non-browser client.
	default: // "same-site", "cross-site"
		return false
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, host)
}

// unauthorized sends a 401 response with the WWW-Authenticate header.
func unauthorized(c *fiber.Ctx) error {
	c.Set("WWW-Authenticate", `Basic realm="Homeland Dashboard"`)
	return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
}
