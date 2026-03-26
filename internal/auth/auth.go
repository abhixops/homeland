// Package auth provides optional Basic HTTP authentication middleware for Fiber.
package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Config holds the authentication configuration.
type Config struct {
	Enabled  bool
	Username string
	Password string
}

// Middleware returns a Fiber middleware that enforces HTTP Basic Auth
// when authentication is enabled. If disabled, it passes through.
func Middleware(cfg *Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
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

// unauthorized sends a 401 response with the WWW-Authenticate header.
func unauthorized(c *fiber.Ctx) error {
	c.Set("WWW-Authenticate", `Basic realm="Homeland Dashboard"`)
	return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
}
