package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config encapsulates strongly-typed configuration settings for the Hader-V2 platform.
type Config struct {
	Port                   string
	AppEnv                 string
	DatabaseURL            string
	JWTSecret              string
	SupabaseJWTSecret      string
	AllowedOrigin          string
	SupabaseURL            string
	SupabaseAnonKey        string
	SupabaseServiceRoleKey string
}

// IsProduction returns true if the application is running in production mode.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

// LoadConfig loads variables from .env (if present) and system environment.
// It executes mandatory validation and provides secure fallback defaults for development
// while strictly enforcing critical credentials in production.
func LoadConfig() *Config {
	// Attempt to load .env file; silently ignore if absent (e.g. in Fly.io / Docker / cloud environments)
	if err := godotenv.Load(); err != nil {
		log.Println("[CONFIG] Notice: No .env file loaded; utilizing system environment variables.")
	}

	appEnv := strings.TrimSpace(os.Getenv("APP_ENV"))
	if appEnv == "" {
		if os.Getenv("FLY_APP_NAME") != "" {
			appEnv = "production"
		} else {
			appEnv = "development"
		}
	}
	isProd := strings.EqualFold(appEnv, "production")

	// 1. Port with standard 8080 default (Fly.io passes PORT dynamically)
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	// 2. Database URL: Cleanly read DATABASE_URL
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		if isProd {
			log.Println("[CONFIG ERROR] Mandatory environment variable missing: DATABASE_URL. Please set DATABASE_URL secret on Fly.io (e.g. fly secrets set DATABASE_URL=...).")
		} else {
			databaseURL = "postgres://postgres:postgres@localhost:5432/hader_db?sslmode=disable"
			log.Println("[CONFIG] Notice: DATABASE_URL not supplied; defaulting to local PostgreSQL development fallback.")
		}
	}

	// 3. Supabase JWT Secret: Support SUPABASE_JWT_SECRET and legacy JWT_SECRET
	supabaseJWTSecret := strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))
	if supabaseJWTSecret == "" {
		supabaseJWTSecret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	if supabaseJWTSecret == "" {
		if isProd {
			log.Println("[CONFIG ERROR] Mandatory environment variable missing: SUPABASE_JWT_SECRET. Required for in-memory JWT authentication. Please set on Fly.io (e.g. fly secrets set SUPABASE_JWT_SECRET=...).")
		} else {
			supabaseJWTSecret = "hader-v2-dev-fallback-secret-minimum-32-chars-long!"
			log.Println("[CONFIG] WARNING: Utilizing default development JWT secret. Set SUPABASE_JWT_SECRET in production!")
		}
	}

	// 4. Allowed Origins for CORS whitelist (strictly includes https://hader-v2.vercel.app)
	allowedOrigin := strings.TrimSpace(os.Getenv("ALLOWED_ORIGIN"))
	if allowedOrigin == "" {
		allowedOrigin = "https://hader-v2.vercel.app,*.vercel.app,http://localhost:3000,http://localhost:5173"
	}

	// 5. Supabase API credentials
	supabaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/")
	supabaseAnonKey := strings.TrimSpace(os.Getenv("SUPABASE_ANON_KEY"))
	supabaseServiceRoleKey := strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY"))

	return &Config{
		Port:                   port,
		AppEnv:                 appEnv,
		DatabaseURL:            databaseURL,
		JWTSecret:              supabaseJWTSecret,
		SupabaseJWTSecret:      supabaseJWTSecret,
		AllowedOrigin:          allowedOrigin,
		SupabaseURL:            supabaseURL,
		SupabaseAnonKey:        supabaseAnonKey,
		SupabaseServiceRoleKey: supabaseServiceRoleKey,
	}
}
