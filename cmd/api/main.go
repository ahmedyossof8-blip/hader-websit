package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"hader-backend/internal/config"
	"hader-backend/internal/database"
	"hader-backend/internal/middleware"
	"hader-backend/internal/modules/attendance"
	"hader-backend/internal/modules/auth"
	"hader-backend/internal/modules/sessions"
	"hader-backend/internal/workers"
)

var appStartTime = time.Now()

func main() {
	// 1. Load strongly-typed environment configuration
	cfg := config.LoadConfig()

	// 2. Cleanly extract and validate critical environment variables
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		dbURL = cfg.DatabaseURL
	}
	if dbURL == "" {
		log.Fatalf("[FATAL] Mandatory environment variable missing: DATABASE_URL. Please set DATABASE_URL secret on Fly.io (fly secrets set DATABASE_URL=...)")
	}

	supabaseJWTSecret := strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))
	if supabaseJWTSecret == "" {
		supabaseJWTSecret = cfg.SupabaseJWTSecret
	}
	if supabaseJWTSecret == "" {
		log.Fatalf("[FATAL] Mandatory environment variable missing: SUPABASE_JWT_SECRET. Please set SUPABASE_JWT_SECRET secret on Fly.io (fly secrets set SUPABASE_JWT_SECRET=...)")
	}

	// 3. Initialize Supabase PostgreSQL connection pool (pgxpool)
	pool, err := database.ConnectDB(dbURL)
	if err != nil {
		log.Fatalf("[FATAL] Could not initialize Supabase database pool: %v\nPlease verify DATABASE_URL and Supabase pooler availability.", err)
	}
	defer database.CloseDB(pool)

	// 4. Initialize Go Fiber Application with optimized zero-allocation error handler
	app := fiber.New(fiber.Config{
		AppName:      "Hader-V2 Academic Attendance Engine",
		ServerHeader: "Hader-Engine",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				code = fiberErr.Code
			}

			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    code,
					"message": err.Error(),
				},
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		},
	})

	// 5. Register Middlewares in production-ready logical order:
	// 1) Panic Recovery: Catch unhandled panics and prevent process crashes
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	// 2) HTTP Access Logger: Log all requests including CORS and Rate Limit rejections
	app.Use(logger.New(logger.Config{
		Format:     "[${time}] ${status} - ${latency} ${method} ${path}\n",
		TimeFormat: "2006-01-02 15:04:05",
	}))

	// 3) Strict CORS Whitelist: Handles OPTIONS preflight requests & allows https://hader-v2.vercel.app
	app.Use(middleware.StrictCORS(cfg.AllowedOrigin))

	// 4) Production Security Headers: Anti-clickjacking, HSTS, camera/geolocation permissions
	app.Use(middleware.SecurityHeaders())

	// 5) Global Rate Limiter: Baseline DDoS protection (skips health checks and preflight OPTIONS)
	app.Use(middleware.GlobalLimiter())

	// 6. Register Health & Diagnostic Endpoints (Used by Fly.io and Docker HEALTHCHECK)
	app.Get("/health", func(c *fiber.Ctx) error {
		pingCtx, pingCancel := context.WithTimeout(c.Context(), 3*time.Second)
		defer pingCancel()

		dbStart := time.Now()
		dbErr := pool.Ping(pingCtx)
		dbLatency := time.Since(dbStart)

		dbStatus := "connected"
		httpStatus := fiber.StatusOK
		if dbErr != nil {
			dbStatus = fmt.Sprintf("unreachable: %v", dbErr)
			httpStatus = fiber.StatusServiceUnavailable
		}

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		poolStat := pool.Stat()

		return c.Status(httpStatus).JSON(fiber.Map{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"uptime":    time.Since(appStartTime).String(),
			"database": fiber.Map{
				"status":  dbStatus,
				"latency": fmt.Sprintf("%dms", dbLatency.Milliseconds()),
				"pool": fiber.Map{
					"total_conns":    poolStat.TotalConns(),
					"idle_conns":     poolStat.IdleConns(),
					"acquired_conns": poolStat.AcquiredConns(),
					"max_conns":      poolStat.MaxConns(),
				},
			},
			"system": fiber.Map{
				"goroutines": runtime.NumGoroutine(),
				"alloc_mb":   fmt.Sprintf("%.2f MB", float64(mem.Alloc)/1024/1024),
				"sys_mb":     fmt.Sprintf("%.2f MB", float64(mem.Sys)/1024/1024),
				"num_gc":     mem.NumGC,
			},
			"environment": cfg.AppEnv,
		})
	})

	// Root ping
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "healthy",
			"message": "Hader-V2 High-Performance Backend Engine is Operational",
			"version": "2.0.0",
		})
	})

	// 7. Initialize Domain Modules & Dependency Injection
	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(authRepo, cfg)
	authHandler := auth.NewHandler(authService, supabaseJWTSecret)

	attendanceRepo := attendance.NewRepository(pool)
	attendanceService := attendance.NewService(attendanceRepo)
	attendanceHandler := attendance.NewHandler(attendanceService, supabaseJWTSecret)

	sessionRepo := sessions.NewRepository(pool)
	sessionService := sessions.NewService(sessionRepo)
	sessionHandler := sessions.NewHandler(sessionService, supabaseJWTSecret)

	// API v1 Routing
	api := app.Group("/api/v1")
	authGroup := api.Group("/auth")
	authHandler.RegisterRoutes(authGroup)
	attendanceHandler.RegisterRoutes(api)
	sessionHandler.RegisterRoutes(api)

	// Diagnostic protected ping verifying JWT pipeline end-to-end
	authGroup.Get("/protected-ping", middleware.Authenticate(supabaseJWTSecret), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"success":   true,
			"message":   "Supabase JWT verified successfully",
			"user_id":   middleware.GetUserID(c),
			"email":     middleware.GetUserEmail(c),
			"role":      middleware.GetUserRole(c),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// 8. Initialize Background Workers (Auto-Session Closure & Daily Streak Calculator)
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()

	go workers.StartSessionWatcher(workerCtx, pool, 30*time.Second)
	go workers.StartDailyStreakCalculator(workerCtx, pool)

	// 9. Host and Port Binding: Dynamically read Fly.io's PORT environment variable, fallback to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	address := "0.0.0.0:" + port

	// 10. Graceful Teardown Handler (Intercept SIGINT/SIGTERM for clean container shutdowns)
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-shutdownChan
		log.Printf("[SERVER] Intercepted shutdown signal (%v). Commencing graceful teardown...", sig)
		cancelWorkers()

		log.Printf("[SERVER] Draining active HTTP connections...")
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Printf("[SERVER] Error during Fiber graceful shutdown: %v", err)
		}
	}()

	// 11. Start Server on 0.0.0.0:PORT
	log.Printf("Starting server on %s", address)
	if err := app.Listen(address); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed to start: %v", err)
	}

	log.Println("[SERVER] Shutdown sequence finalized cleanly.")
}
