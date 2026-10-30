package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// rateLimitErrorHandler returns a uniform JSON error payload when a rate limit threshold is violated.
func rateLimitErrorHandler(c *fiber.Ctx) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
		"success": false,
		"error": fiber.Map{
			"code":    "RATE_LIMIT_EXCEEDED",
			"message": "Too many requests. Please slow down.",
		},
	})
}

// GlobalLimiter enforces a baseline protection layer of 60 requests per minute per IP.
func GlobalLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        60,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: rateLimitErrorHandler,
	})
}

// AuthLimiter enforces strict brute-force deterrence for sensitive authentication routes
// allowing only 5 attempts per 15 minutes per client IP.
func AuthLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        5,
		Expiration: 15 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: rateLimitErrorHandler,
	})
}

// AttendanceLimiter protects the critical lecture attendance check-in path from spamming
// and concurrent duplicate submissions, allowing at most 3 requests per 10 seconds per user/device/IP.
func AttendanceLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        3,
		Expiration: 10 * time.Second,
		KeyGenerator: func(c *fiber.Ctx) string {
			// Prioritize authenticated student UUID if present in request context
			if userID, ok := c.Locals("user_id").(string); ok && userID != "" {
				return "user:" + userID
			}
			// Secondarily prioritize hardware device ID header
			if deviceID := c.Get("X-Device-Id"); deviceID != "" {
				return "dev:" + deviceID
			}
			// Fallback to client IP
			return "ip:" + c.IP()
		},
		LimitReached: rateLimitErrorHandler,
	})
}
