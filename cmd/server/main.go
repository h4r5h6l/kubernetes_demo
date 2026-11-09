package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/h4r5h6l/kubernetes_demo/internal/config"
	"github.com/h4r5h6l/kubernetes_demo/internal/database"
	"github.com/h4r5h6l/kubernetes_demo/internal/httpx"
	"github.com/h4r5h6l/kubernetes_demo/internal/task"
	"github.com/h4r5h6l/kubernetes_demo/migrations"
	"github.com/h4r5h6l/kubernetes_demo/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	healthcheckOnly := flag.Bool("healthcheck", false, "request a probe endpoint on localhost:$PORT and exit")
	healthcheckPath := flag.String("healthcheck-path", "/health", "probe path used with -healthcheck")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	if *healthcheckOnly {
		return checkHealth(cfg.Port, *healthcheckPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.ConnectWithRetry(ctx, cfg.DatabaseURL, 30, 2*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/health", httpx.Health)
	router.GET("/ready", httpx.Ready(pool))

	taskRepo := task.NewPostgresRepository(pool)
	taskHandler := task.NewHandler(taskRepo)
	taskHandler.RegisterRoutes(router)
	taskHandler.RegisterRoutes(router.Group("/api/v1"))

	webHandler, err := web.NewHandler(taskRepo)
	if err != nil {
		return fmt.Errorf("web handler: %w", err)
	}
	webHandler.RegisterRoutes(router)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s (env=%s)", srv.Addr, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Println("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Println("server stopped")
	return nil
}

func checkHealth(port, path string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + path)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: unexpected status %d", resp.StatusCode)
	}
	return nil
}
