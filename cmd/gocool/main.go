// Package main is the entry point for the GoCool application.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/user/gocool/internal/config"
	"github.com/user/gocool/internal/database"
	"github.com/user/gocool/internal/logger"
	"github.com/user/gocool/internal/web"
)

var version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to load .env file", "error", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logging
	log := logger.Setup(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting gocool",
		"version", version,
		"dev_mode", cfg.DevMode,
		"database_type", cfg.DatabaseType,
	)

	// Connect to database
	db, err := database.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	log.Info("database connected",
		"type", cfg.DatabaseType,
		"path", cfg.DatabasePath(),
	)

	// Run migrations
	migrator, err := database.NewMigrator(db)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	defer migrator.Close()

	if err = migrator.Up(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	mVersion, dirty, err := migrator.Version()
	if err != nil {
		log.Warn("could not get migration version", slog.Any("error", err))
	} else {
		log.Info("migrations complete", "version", mVersion, "dirty", dirty)
	}

	// Create router
	router := web.NewRouter(cfg)

	// Create HTTP server
	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for errors from the server
	serverErrors := make(chan error, 1)

	// Start the server
	go func() {
		log.Info("server listening", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}

	case sig := <-shutdown:
		log.Info("shutdown signal received", "signal", sig.String())

		// Give outstanding requests time to complete
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Gracefully shutdown the server
		if err := server.Shutdown(ctx); err != nil {
			log.Error("graceful shutdown failed", slog.Any("error", err))
			if err := server.Close(); err != nil {
				return fmt.Errorf("forced shutdown failed: %w", err)
			}
		}

		log.Info("server stopped gracefully")
	}

	return nil
}
