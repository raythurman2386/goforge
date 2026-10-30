// Package main is the entry point for the GoForge application.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/raythurman2386/goforge/internal/apiv1"
	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/logger"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/secrets"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/sse"
	"github.com/raythurman2386/goforge/internal/web"
	"github.com/raythurman2386/goforge/internal/web/handlers"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/middleware"
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

// version is reported in logs, the API, and backup manifests.
const version = "0.1.0"

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backups to S3-compatible storage",
}

var backupRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run one backup now (same as the nightly run, trigger=manual)",
	Long: `Run one backup now and wait for it to finish.

Uses the same BACKUP_* settings and lock as the nightly run inside
"goforge serve", so the two never overlap. It does not run migrations; the
backup_runs table comes from "goforge serve" or "goforge migrate up".
Exits non-zero if the run failed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupOnce(cmd.Context())
	},
}

func runBackupOnce(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	log := logger.Setup(cfg.LogLevel, cfg.LogFormat)
	db, err := database.New(cfg) //nolint:contextcheck // database.New takes no context
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	setup, err := backup.New(cfg, db.DB, dockerClient, logger.WithComponent(log, "backup"), version)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	run, err := setup.Runner.Run(ctx, models.BackupTriggerManual)
	if err != nil {
		return err
	}
	fmt.Printf("backup %s: status=%s key=%s%s objects=%d bytes=%d skipped=%d pruned=%d\n",
		run.ID, run.Status, cfg.Backup.Prefix, run.RunKey, run.Objects, run.Bytes, run.Skipped, run.Pruned)
	if run.ErrorSummary.Valid {
		fmt.Printf("note: %s\n", run.ErrorSummary.String)
	}
	if run.Status != models.BackupRunSucceeded {
		return errors.New("backup run failed")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(backupCmd)
	backupCmd.AddCommand(backupRunCmd)
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
		"version", version,
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
	secretService, err := secrets.NewEncryptorFromString(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("failed to create encryption service: %w", err)
	}

	userRepo := repositories.NewUserRepository(db.DB, secretService).WithAdminEmail(cfg.AdminEmail)
	grantCtx, grantCancel := context.WithTimeout(context.Background(), 5*time.Second)
	grantErr := userRepo.GrantAdmin(grantCtx, cfg.AdminEmail)
	grantCancel()
	if grantErr != nil && !errors.Is(grantErr, repositories.ErrNotFound) {
		return fmt.Errorf("failed to grant admin: %w", grantErr)
	}
	if cfg.AdminEmail != "" && errors.Is(grantErr, repositories.ErrNotFound) {
		log.Info("admin email has no account yet", "email", cfg.AdminEmail)
	}
	sessionRepo := repositories.NewSessionRepository(db.DB)
	projectRepo := repositories.NewProjectRepository(db.DB)
	environmentRepo := repositories.NewEnvironmentRepository(db.DB)
	envVariableRepo := repositories.NewEnvVariableRepository(db.DB, secretService)
	containerRepo := repositories.NewContainerRepository(db.DB)
	deploymentRepo := repositories.NewDeploymentRepository(db.DB)
	deploymentLogRepo := repositories.NewDeploymentLogRepository(db.DB)
	instanceRepo := repositories.NewServiceInstanceRepository(db.DB).WithEncryptor(secretService)
	projectServiceInstanceRepo := repositories.NewProjectServiceInstanceRepository(db.DB).WithEncryptor(secretService)
	auditLogRepo := repositories.NewAuditLogRepository(db.DB)
	gitSourceRepo := repositories.NewGitSourceRepository(db.DB, secretService)
	queueRepo := repositories.NewQueueRepository(db.DB)

	// Initialize clients
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	gitService := git.NewService()
	sseHub := sse.NewHub()
	go sseHub.Run()

	// Nightly backups run inside serve. backgroundCtx is canceled on
	// shutdown; an in-flight run then records itself as failed.
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backupDone := make(chan struct{})
	defer func() {
		cancelBackground()
		select {
		case <-backupDone:
		case <-time.After(15 * time.Second):
			log.Warn("backup goroutine did not stop in time")
		}
	}()
	backupRuns := repositories.NewBackupRunRepository(db.DB)
	backupStatus := &backup.StatusReader{Config: cfg.Backup, Runs: backupRuns}
	startBackups(backgroundCtx, backupDone, cfg, db.DB, dockerClient, logger.WithComponent(log, "backup"))

	monitorCollector := monitor.NewCollector(dockerClient, containerRepo, projectRepo, instanceRepo)

	// Initialize services
	auditService := services.NewAuditService(auditLogRepo, logger.WithComponent(log, "audit"))
	sessionManager := auth.NewSessionManager(sessionRepo, userRepo, logger.WithComponent(log, "auth"))
	// Delete expired session rows at startup and hourly; backgroundCtx stops it.
	go sessionManager.RunExpiredSessionPurge(backgroundCtx, auth.SessionPurgeInterval)
	githubProvider := auth.NewGitHubProvider(cfg)
	authenticator := auth.NewLocalAuthenticator(userRepo)
	projectService := project.NewService(projectRepo, environmentRepo, envVariableRepo, deploymentRepo, containerRepo, gitSourceRepo, gitService, dockerClient)
	deployService := deploy.NewService(
		deploymentRepo,
		deploymentLogRepo,
		projectRepo,
		environmentRepo,
		containerRepo,
		gitSourceRepo,
		envVariableRepo,
		projectServiceInstanceRepo,
		gitService,
		dockerClient,
		sseHub,
		cfg.TraefikNetwork,
		cfg.CloudflareProxyMode,
		cfg.BuildDir,
		logger.WithComponent(log, "deploy"),
	)

	// Initialize deployment worker
	deployWorker := deploy.NewWorker(deployService, queueRepo, 0, 0, cfg.BuildTimeout)
	deployWorker.Start()
	defer deployWorker.Stop()

	rollbackManager := deploy.NewRollbackManager(deployService)

	// Initialize service catalog
	catalog := services.NewCatalog(cfg.TemplatesDir)
	catalog.SetLogger(logger.WithComponent(log, "catalog"))
	if err := catalog.LoadTemplates(); err != nil {
		log.Error("failed to load service templates", "error", err)
	}
	instanceService := services.NewInstanceService(instanceRepo, containerRepo, dockerClient, catalog, cfg.TraefikNetwork, cfg.CloudflareProxyMode, logger.WithComponent(log, "instance"))

	// Initialize handlers
	sseHandler := handlers.NewSSEHandler(sseHub, dockerClient, monitorCollector, containerRepo, instanceRepo, deployService, projectService, logger.WithComponent(log, "sse"))
	// Lock out password logins after repeated failures per client IP and per
	// account (stopped during graceful shutdown).
	loginThrottle := middleware.NewDefaultLoginThrottle()
	defer loginThrottle.Stop()
	authHandler := handlers.NewAuthHandler(sessionManager, userRepo, githubProvider, authenticator, auditService, cfg.EnableRegistration, logger.WithComponent(log, "auth")).
		WithLoginThrottle(loginThrottle)
	dashboardHandler := handlers.NewDashboardHandler(monitorCollector, instanceService, projectRepo, gitSourceRepo, logger.WithComponent(log, "dashboard"))
	projectHandler := handlers.NewProjectHandler(projectService, gitSourceRepo, deploymentRepo, containerRepo, projectServiceInstanceRepo, instanceService, logger.WithComponent(log, "projects"))
	envHandler := handlers.NewEnvironmentHandler(projectService, logger.WithComponent(log, "environments"))
	deploymentHandler := handlers.NewDeploymentHandler(deployService, projectService, rollbackManager, deployWorker, logger.WithComponent(log, "deployments"))
	serviceHandler := handlers.NewServiceHandler(catalog, instanceService, logger.WithComponent(log, "services"))
	apiTokenRepo := repositories.NewAPITokenRepository(db.DB)
	settingsHandler := handlers.NewSettingsHandler(gitSourceRepo, instanceService, userRepo, apiTokenRepo, logger.WithComponent(log, "settings")).WithBackups(backupStatus)
	gitHandler := handlers.NewGitHandler(gitSourceRepo, gitService, logger.WithComponent(log, "git"))
	webhookHandler := webhook.NewHandler(deployService, projectRepo, environmentRepo, deployWorker, cfg.WebhookSecret, logger.WithComponent(log, "webhook"))
	healthHandler := handlers.NewHealthHandler(db, dockerClient, logger.WithComponent(log, "health"))
	containerHandler := handlers.NewContainerHandler(containerRepo, instanceRepo, dockerClient, logger.WithComponent(log, "containers"))

	// Rate limiter (stopped during graceful shutdown)
	// CF-Connecting-IP is only read with CLOUDFLARE_PROXY_MODE (see httputil.ProxyTrust).
	proxyTrust := httputil.NewProxyTrust(cfg.TrustedProxies, cfg.CloudflareProxyMode)
	rateLimiter := middleware.NewRateLimiter(30, 60, proxyTrust)
	defer rateLimiter.Stop()

	// JSON API v1 for the GoForge MCP server (bearer tokens with scopes).
	apiV1 := apiv1.New(apiv1.Deps{
		Tokens:      apiTokenRepo,
		Users:       userRepo,
		Projects:    projectService,
		Deployments: deployService,
		DeployList:  deploymentRepo,
		Queue:       deployWorker,
		Rollback:    rollbackManager,
		Instances:   instanceService,
		Catalog:     catalog,
		Links:       projectServiceInstanceRepo,
		Containers:  containerRepo,
		Docker:      dockerClient,
		Stats:       monitorCollector,
		Audit:       auditLogRepo,
		GitSources:  gitSourceRepo,
		GitBrowser:  gitService,
		DB:          db,
		Backups:     backupStatus,
		Logger:      logger.WithComponent(log, "api_v1"),
		Version:     version,
		ClientIP: func(r *http.Request) string {
			return httputil.ClientIP(r, proxyTrust)
		},
		ProtectedServices: cfg.ProtectedServices,
	}).Routes()

	// Create router
	router := web.Router(
		cfg,
		sessionManager,
		apiTokenRepo,
		userRepo,
		rateLimiter,
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
		healthHandler,
		containerHandler,
		apiV1,
	)

	// Configure server
	//
	// WriteTimeout is intentionally 0 (no global deadline) because the server
	// hosts SSE streaming endpoints (deployment logs, container stats/logs)
	// that are long-lived by design. Per-route write timeouts are applied via
	// middleware.Timeout in the router for all non-streaming handlers.
	srv := &http.Server{
		Addr:        cfg.Addr(),
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
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

// startBackups starts the nightly scheduler when backups are enabled and the
// schedule is not "off". It closes done when the scheduler stops.
func startBackups(ctx context.Context, done chan<- struct{}, cfg *config.Config, db *sql.DB, dockerClient *docker.Client, log *slog.Logger) {
	setup, err := backup.New(cfg, db, dockerClient, log, version)
	switch {
	case errors.Is(err, backup.ErrDisabled):
		log.Info("backups disabled (BACKUP_S3_BUCKET unset)")
		close(done)
		return
	case err != nil:
		// Config.validate already checked these settings, so this is unexpected.
		log.Error("backups not started", "error", backup.RedactorFor(cfg).Redact(err.Error()))
		close(done)
		return
	case !setup.Scheduled:
		log.Info("backups enabled, nightly schedule off; use goforge backup run")
		close(done)
		return
	}
	go func() {
		defer close(done)
		setup.Runner.Schedule(ctx, setup.Schedule)
	}()
}
