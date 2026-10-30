package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// SecurityHeaders returns a high-performance middleware applying strict HTTP security headers
// to protect against Clickjacking, MIME confusion, XSS, and unauthorized hardware access.
func SecurityHeaders() fiber.Handler {
	const (
		xFrameOptions           = "DENY"
		xContentTypeOptions     = "nosniff"
		crossOriginOpenerPolicy = "same-origin"
		hsts                    = "max-age=63072000; includeSubDomains; preload"
		permissionsPolicy       = "camera=(self), geolocation=(self), microphone=(), payment=()"
		contentSecurityPolicy   = "default-src 'self'; script-src 'self' 'unsafe-inline' https://vercel.live; connect-src 'self' https://*.supabase.co https://*.pooler.supabase.com wss://*.supabase.co; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none';"
	)

	return func(c *fiber.Ctx) error {
		// Prevent Clickjacking
		c.Set("X-Frame-Options", xFrameOptions)
		// Prevent MIME-sniffing
		c.Set("X-Content-Type-Options", xContentTypeOptions)
		// Cross-Origin Opener Policy (COOP) isolation
		c.Set("Cross-Origin-Opener-Policy", crossOriginOpenerPolicy)
		// HTTP Strict Transport Security (HSTS)
		c.Set("Strict-Transport-Security", hsts)
		// First-party hardware enforcement (Camera & Geolocation allowed for Hader-V2)
		c.Set("Permissions-Policy", permissionsPolicy)
		// Content-Security-Policy
		c.Set("Content-Security-Policy", contentSecurityPolicy)

		return c.Next()
	}
}
