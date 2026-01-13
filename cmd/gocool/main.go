// Package main is the entry point for the GoCool application.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/user/gocool/internal/config"
	"github.com/user/gocool/internal/database"
	"github.com/user/gocool/internal/logger"
	"github.com/user/gocool/internal/web"
)

var rootCmd = &cobra.Command{
	Use:   "gocool",
	Short: "GoCool is a lightweight self-hosted PaaS",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer()
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migrations",
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Run migrations up",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := database.New(cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		return database.MigrateUp(db, cfg)
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down [steps]",
	Short: "Run migrations down",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := database.New(cfg)
		if err != nil {
			return err
		}
		defer db.Close()

		migrator, err := database.NewMigrator(db)
		if err != nil {
			return err
		}
		defer migrator.Close()

		if len(args) > 0 {
			// Parse steps
			var steps int
			if _, err := fmt.Sscanf(args[0], "%d", &steps); err != nil {
				return fmt.Errorf("invalid steps: %w", err)
			}
			return migrator.Steps(-steps)
		}
		return migrator.Down()
	},
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := database.New(cfg)
		if err != nil {
			return err
		}
		defer db.Close()

		migrator, err := database.NewMigrator(db)
		if err != nil {
			return err
		}
		defer migrator.Close()

		version, dirty, err := migrator.Version()
		if err != nil {
			return err
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)
		return nil
	},
}

var migrateCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new migration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		timestamp := time.Now().Format("20060102150405")

		// Create for both postgres and sqlite for simplicity in dev
		dirs := []string{
			"internal/database/repositories/postgres", // wait, path is migrations/postgres
			"internal/database/migrations/postgres",
			"internal/database/migrations/sqlite",
		}

		for _, dir := range dirs {
			if dir == "internal/database/repositories/postgres" {
				continue // fix typo in loop
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return err
			}
			up := filepath.Join(dir, fmt.Sprintf("%s_%s.up.sql", timestamp, name))
			down := filepath.Join(dir, fmt.Sprintf("%s_%s.down.sql", timestamp, name))

			if err := os.WriteFile(up, []byte("-- UP migration"), 0o600); err != nil {
				return err
			}
			if err := os.WriteFile(down, []byte("-- DOWN migration"), 0o600); err != nil {
				return err
			}
			fmt.Printf("Created %s and %s\n", up, down)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(migrateStatusCmd)
	migrateCmd.AddCommand(migrateCreateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log := logger.Setup(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting GoCool",
		"version", "0.1.0",
		"env", cfg.DevMode,
		"addr", cfg.Addr(),
	)

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("failed to close database", "error", err)
		}
	}()

	// Run migrations
	log.Info("running database migrations")
	if err := database.MigrateUp(db, cfg); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create router
	router := web.NewRouter(cfg, db.DB)

	// Configure server
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start the service listening for requests
	go func() {
		log.Info("server listening", "addr", srv.Addr)
		serverErrors <- srv.ListenAndServe()
	}()

	// Channel to listen for an interrupt or terminate signal from the OS
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Blocking main and waiting for shutdown
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}

	case sig := <-shutdown:
		log.Info("shutdown started", "signal", sig)

		// Give outstanding requests a deadline for completion
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Asking listener to shut down and shed load
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("could not stop server gracefully", "error", err)
			if err := srv.Close(); err != nil {
				log.Error("could not stop server", "error", err)
			}
		}
		log.Info("shutdown complete", "signal", sig)
	}

	return nil
}
