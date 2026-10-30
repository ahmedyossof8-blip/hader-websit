package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func generateTestJWT(secret string, userID string, role string, email string, exp time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"role":  role,
		"app_metadata": map[string]interface{}{
			"role": role,
		},
		"exp": time.Now().Add(exp).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func TestAuthenticateMiddleware(t *testing.T) {
	jwtSecret := "super-secure-hader-test-secret-key-12345"
	app := fiber.New()

	app.Use(Authenticate(jwtSecret))
	app.Get("/test-auth", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"user_id": GetUserID(c),
			"role":    GetUserRole(c),
			"email":   GetUserEmail(c),
		})
	})

	// Case 1: Missing Authorization Header -> 401
	req1 := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
	resp1, err := app.Test(req1)
	if err != nil || resp1.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Expected 401 for missing header, got %d", resp1.StatusCode)
	}

	// Case 2: Invalid/Tampered Token -> 401
	req2 := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
	req2.Header.Set("Authorization", "Bearer invalid.jwt.token")
	resp2, err := app.Test(req2)
	if err != nil || resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Expected 401 for invalid token, got %d", resp2.StatusCode)
	}

	// Case 3: Valid Token -> 200 with extracted claims
	validToken, err := generateTestJWT(jwtSecret, "user-uuid-1234", "class_leader", "leader@hader.edu", 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
	req3.Header.Set("Authorization", "Bearer "+validToken)
	resp3, err := app.Test(req3)
	if err != nil || resp3.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for valid token, got %d", resp3.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp3.Body).Decode(&body)
	if body["user_id"] != "user-uuid-1234" || body["role"] != "class_leader" {
		t.Errorf("Extracted claims mismatch: %v", body)
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	jwtSecret := "super-secure-hader-test-secret-key-12345"
	app := fiber.New()

	app.Use(Authenticate(jwtSecret))
	app.Get("/leader-only", RequireRole("class_leader", "admin"), func(c *fiber.Ctx) error {
		return c.SendString("success")
	})

	// Student role attempting to access leader route -> 403 Forbidden
	studentToken, _ := generateTestJWT(jwtSecret, "student-1", "student", "student@hader.edu", 1*time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/leader-only", nil)
	req.Header.Set("Authorization", "Bearer "+studentToken)
	resp, _ := app.Test(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 for student role on leader route, got %d", resp.StatusCode)
	}

	// Leader role accessing leader route -> 200 OK
	leaderToken, _ := generateTestJWT(jwtSecret, "leader-1", "class_leader", "leader@hader.edu", 1*time.Hour)
	req2 := httptest.NewRequest(http.MethodGet, "/leader-only", nil)
	req2.Header.Set("Authorization", "Bearer "+leaderToken)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for leader role on leader route, got %d", resp2.StatusCode)
	}
}

func TestStrictCORSMiddleware(t *testing.T) {
	app := fiber.New()
	app.Use(StrictCORS("https://hader-v2.vercel.app"))
	app.Get("/cors-test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Allowed Origin
	req1 := httptest.NewRequest(http.MethodGet, "/cors-test", nil)
	req1.Header.Set("Origin", "https://hader-v2.vercel.app")
	resp1, _ := app.Test(req1)
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for allowed origin, got %d", resp1.StatusCode)
	}
	if resp1.Header.Get("Access-Control-Allow-Origin") != "https://hader-v2.vercel.app" {
		t.Errorf("Expected Access-Control-Allow-Origin header to match")
	}

	// Disallowed Origin -> 403 Forbidden
	req2 := httptest.NewRequest(http.MethodGet, "/cors-test", nil)
	req2.Header.Set("Origin", "https://malicious-phishing-site.com")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 for malicious origin, got %d", resp2.StatusCode)
	}
}
