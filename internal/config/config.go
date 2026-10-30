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
	// Attempt to load .env file; silently ignore if absent (e.g. in cloud or CI/CD environments)
	if err := godotenv.Load(); err != nil {
		log.Println("[CONFIG] Notice: No .env file loaded; utilizing system environment variables.")
	}

	appEnv := strings.TrimSpace(os.Getenv("APP_ENV"))
	if appEnv == "" {
		appEnv = "development"
	}
	isProd := strings.EqualFold(appEnv, "production")

	// 1. Port with standard 8080 default
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	// 2. Database URL with production guard and local development fallback
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		if isProd {
			log.Fatal("[FATAL CONFIG] Mandatory environment variable missing: DATABASE_URL. Please specify a valid Supabase PostgreSQL connection string.")
		} else {
			databaseURL = "postgres://postgres:postgres@localhost:5432/hader_db?sslmode=disable"
			log.Println("[CONFIG] Notice: DATABASE_URL not supplied; defaulting to local PostgreSQL fallback.")
		}
	}

	// 3. JWT Secret: Support both JWT_SECRET and SUPABASE_JWT_SECRET
	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if jwtSecret == "" {
		jwtSecret = strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))
	}
	if jwtSecret == "" {
		if isProd {
			log.Fatal("[FATAL CONFIG] Mandatory environment variable missing: JWT_SECRET (or SUPABASE_JWT_SECRET). Required for in-memory JWT authentication.")
		} else {
			jwtSecret = "hader-v2-dev-fallback-secret-minimum-32-chars-long!"
			log.Println("[CONFIG] WARNING: Utilizing default development JWT secret. Set JWT_SECRET in production!")
		}
	}

	// 4. Allowed Origins for CORS whitelist
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
		JWTSecret:              jwtSecret,
		SupabaseJWTSecret:      jwtSecret,
		AllowedOrigin:          allowedOrigin,
		SupabaseURL:            supabaseURL,
		SupabaseAnonKey:        supabaseAnonKey,
		SupabaseServiceRoleKey: supabaseServiceRoleKey,
	}
}

