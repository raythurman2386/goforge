// Package main is the entry point for the GoForge application.
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

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/logger"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/sse"
	"github.com/raythurman2386/goforge/internal/web"
	"github.com/raythurman2386/goforge/internal/web/handlers"
	"github.com/raythurman2386/goforge/internal/webhook"
)

var rootCmd = &cobra.Command{
	Use:   "goforge",
	Short: "GoForge is a lightweight self-hosted PaaS",
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
	log.Info("starting GoForge",
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
		if closeErr := db.Close(); closeErr != nil {
			log.Error("failed to close database", "error", closeErr)
		}
	}()

	// Run migrations
	log.Info("running database migrations")
	if migErr := database.MigrateUp(db, cfg); migErr != nil {
		return fmt.Errorf("failed to run migrations: %w", migErr)
	}

	// Initialize repositories
	userRepo := repositories.NewUserRepository(db.DB)
	sessionRepo := repositories.NewSessionRepository(db.DB)
	projectRepo := repositories.NewProjectRepository(db.DB)
	environmentRepo := repositories.NewEnvironmentRepository(db.DB)
	envVariableRepo := repositories.NewEnvVariableRepository(db.DB)
	containerRepo := repositories.NewContainerRepository(db.DB)
	deploymentRepo := repositories.NewDeploymentRepository(db.DB)
	deploymentLogRepo := repositories.NewDeploymentLogRepository(db.DB)
	instanceRepo := repositories.NewServiceInstanceRepository(db.DB)
	gitSourceRepo := repositories.NewGitSourceRepository(db.DB)

	// Initialize clients
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	gitService := git.NewService()
	sseHub := sse.NewHub()
	go sseHub.Run()

	monitorCollector := monitor.NewCollector(dockerClient, containerRepo)
	sseHandler := handlers.NewSSEHandler(sseHub, dockerClient, monitorCollector)

	// Initialize services
	sessionManager := auth.NewSessionManager(sessionRepo, userRepo)
	githubProvider := auth.NewGitHubProvider(cfg)
	authenticator := auth.NewLocalAuthenticator(userRepo)
	projectService := project.NewService(projectRepo, environmentRepo, envVariableRepo)
	deployService := deploy.NewService(
		deploymentRepo,
		deploymentLogRepo,
		projectRepo,
		environmentRepo,
		containerRepo,
		gitService,
		dockerClient,
		sseHub,
		cfg.TraefikNetwork,
	)

	// Initialize worker
	deployWorker := deploy.NewWorker(deployService, cfg.MaxConcurrentBuilds)
	deployWorker.Start()
	defer deployWorker.Stop()

	rollbackManager := deploy.NewRollbackManager(deployService)

	// Initialize service catalog
	catalog := services.NewCatalog(cfg.TemplatesDir)
	if err := catalog.LoadTemplates(); err != nil {
		log.Error("failed to load service templates", "error", err)
	}
	instanceService := services.NewInstanceService(instanceRepo, containerRepo, dockerClient, catalog)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(sessionManager, userRepo, githubProvider, authenticator)
	dashboardHandler := handlers.NewDashboardHandler()
	projectHandler := handlers.NewProjectHandler(projectService, gitSourceRepo)
	envHandler := handlers.NewEnvironmentHandler(projectService)
	deploymentHandler := handlers.NewDeploymentHandler(deployService, projectService, rollbackManager, deployWorker)
	serviceHandler := handlers.NewServiceHandler(catalog, instanceService)
	settingsHandler := handlers.NewSettingsHandler(gitSourceRepo)
	gitHandler := handlers.NewGitHandler(gitSourceRepo, gitService)
	webhookHandler := webhook.NewHandler(deployService, projectRepo, environmentRepo, deployWorker, cfg.WebhookSecret)

	// Create router
	router := web.Router(
		cfg,
		sessionManager,
		authHandler,
		dashboardHandler,
		projectHandler,
		envHandler,
		deploymentHandler,
		serviceHandler,
		settingsHandler,
		gitHandler,
		webhookHandler,
		sseHandler,
	)

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
