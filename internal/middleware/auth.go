package middleware

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// Authenticate verifies the incoming Supabase JWT token in-memory using HMAC-SHA256.
// It bypasses external network calls to Supabase, guaranteeing sub-millisecond authentication.
func Authenticate(jwtSecret string) fiber.Handler {
	secretBytes := []byte(jwtSecret)

	return func(c *fiber.Ctx) error {
		var tokenString string
		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"success": false,
					"error": fiber.Map{
						"code":    "MALFORMED_TOKEN",
						"message": "Invalid authorization token format. Expected: Bearer <token>",
					},
				})
			}
			tokenString = strings.TrimSpace(parts[1])
		} else if qToken := c.Query("token"); qToken != "" {
			tokenString = strings.TrimSpace(qToken)
		} else {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "UNAUTHORIZED",
					"message": "Missing authorization header or token parameter",
				},
			})
		}

		// Parse and validate JWT token
		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			// Enforce HMAC-SHA256 signing method to prevent algorithm confusion attacks
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secretBytes, nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "INVALID_TOKEN",
					"message": "Authorization token is invalid, expired, or tampered",
				},
			})
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "INVALID_CLAIMS",
					"message": "Malformed token claims payload",
				},
			})
		}

		// Extract standard Supabase claims
		var userID, email, role string

		// 1. User UUID (sub)
		if sub, ok := claims["sub"].(string); ok {
			userID = sub
		}

		// 2. Email
		if em, ok := claims["email"].(string); ok {
			email = em
		}

		// 3. User Role: Check app_metadata -> user_metadata -> top-level claim
		if appMeta, ok := claims["app_metadata"].(map[string]interface{}); ok {
			if r, ok := appMeta["role"].(string); ok && r != "" {
				role = r
			}
		}
		if role == "" {
			if userMeta, ok := claims["user_metadata"].(map[string]interface{}); ok {
				if r, ok := userMeta["role"].(string); ok && r != "" {
					role = r
				}
			}
		}
		if role == "" {
			if r, ok := claims["role"].(string); ok && r != "" {
				role = r
			}
		}

		// Fallback default role if authenticated but unspecified
		if role == "" {
			role = "student"
		}

		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "UNAUTHORIZED_NO_SUBJECT",
					"message": "Token claims lack mandatory subject identifier",
				},
			})
		}

		// Inject verified claims into Fiber request context
		c.Locals("user_id", userID)
		c.Locals("email", email)
		c.Locals("role", role)

		return c.Next()
	}
}

// RequireRole enforces strict Role-Based Access Control (RBAC).
// Only users matching one of the supplied allowedRoles are permitted to proceed.
func RequireRole(allowedRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		currentRole, _ := c.Locals("role").(string)

		hasPermission := false
		for _, allowed := range allowedRoles {
			if strings.EqualFold(currentRole, allowed) {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "FORBIDDEN_ROLE",
					"message": "Access denied: insufficient permissions for this operation",
				},
			})
		}

		return c.Next()
	}
}

// GetUserID retrieves the authenticated user's UUID from Fiber context.
func GetUserID(c *fiber.Ctx) string {
	val, _ := c.Locals("user_id").(string)
	return val
}

// GetUserRole retrieves the authenticated user's role from Fiber context.
func GetUserRole(c *fiber.Ctx) string {
	val, _ := c.Locals("role").(string)
	return val
}

// GetUserEmail retrieves the authenticated user's email from Fiber context.
func GetUserEmail(c *fiber.Ctx) string {
	val, _ := c.Locals("email").(string)
	return val
}
