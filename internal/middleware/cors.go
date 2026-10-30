package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// StrictCORS enforces an impenetrable origin whitelist policy.
// It verifies preflight and regular requests against allowedOrigin,
// enables credentials, allows custom headers (like X-Device-Id), and rejects non-whitelisted origins with 403 Forbidden.
func StrictCORS(allowedOrigin string) fiber.Handler {
	// Support comma-separated origins if provided (e.g., for local testing alongside production)
	allowedList := strings.Split(allowedOrigin, ",")
	for i := range allowedList {
		allowedList[i] = strings.TrimSpace(allowedList[i])
	}

	return func(c *fiber.Ctx) error {
		origin := c.Get("Origin")

		// If no Origin header is supplied (e.g. server-to-server, curl, health monitors), proceed normally
		if origin == "" {
			return c.Next()
		}

		// Validate origin against whitelist
		isAllowed := false
		for _, allowed := range allowedList {
			if allowed == "*" || strings.EqualFold(origin, allowed) {
				isAllowed = true
				break
			}
			// Support wildcard subdomain matching (e.g. *.vercel.app for Vercel preview environments)
			if strings.HasPrefix(allowed, "*.") {
				domainSuffix := strings.TrimPrefix(allowed, "*")
				if strings.HasSuffix(strings.ToLower(origin), strings.ToLower(domainSuffix)) {
					isAllowed = true
					break
				}
			}
		}

		if !isAllowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "CORS_ORIGIN_DENIED",
					"message": "Origin not allowed by Hader-V2 CORS policy",
				},
			})
		}

		// Apply CORS response headers
		c.Set("Access-Control-Allow-Origin", origin)
		c.Set("Vary", "Origin")
		c.Set("Access-Control-Allow-Credentials", "true")
		c.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Device-Id, X-Requested-With, Cache-Control")
		c.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		c.Set("Access-Control-Max-Age", "86400")

		// Handle preflight OPTIONS request
		if c.Method() == fiber.MethodOptions {
			return c.SendStatus(fiber.StatusNoContent)
		}

		return c.Next()
	}
}
