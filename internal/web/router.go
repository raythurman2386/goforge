// Package web provides HTTP routing and handlers.
package web

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/web/handlers"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/middleware"
	"github.com/raythurman2386/goforge/internal/webhook"
)

// Router creates and configures the HTTP router.
func Router(
	cfg *config.Config,
	sessionManager *auth.SessionManager,
	authHandler *handlers.AuthHandler,
	dashboardHandler *handlers.DashboardHandler,
	projectHandler *handlers.ProjectHandler,
	envHandler *handlers.EnvironmentHandler,
	deploymentHandler *handlers.DeploymentHandler,
	serviceHandler *handlers.ServiceHandler,
	settingsHandler *handlers.SettingsHandler,
	gitHandler *handlers.GitHandler,
	webhookHandler *webhook.Handler,
	sseHandler *handlers.SSEHandler,
	healthHandler *handlers.HealthHandler,
	containerHandler *handlers.ContainerHandler,
) http.Handler {
	r := chi.NewRouter()

	// Parse trusted proxy CIDRs from config (defaults to private/loopback networks)
	trustedProxies := httputil.DefaultTrustedProxies()
	if len(cfg.TrustedProxies) > 0 {
		trustedProxies = httputil.ParseTrustedProxies(cfg.TrustedProxies)
	}

	// Initialize handler-level trusted proxies for getClientIP
	handlers.SetTrustedProxies(trustedProxies)

	// Middleware stack
	r.Use(chi_middleware.Recoverer)
	// Rate limiting: 30 rps, burst 60
	r.Use(middleware.RateLimit(30, 60, trustedProxies))
	// Session middleware is safe for all routes (just populates context)
	r.Use(auth.Middleware(sessionManager))

	// Global error handlers
	r.NotFound(handlers.NotFoundHandler)
	r.MethodNotAllowed(handlers.MethodNotAllowedHandler)

	// Webhooks (No CSRF protection needed/wanted as they are machine-to-machine)
	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/github", webhookHandler.HandleGitHub)
		r.Post("/gitlab", webhookHandler.HandleGitLab)
		r.Post("/gitea", webhookHandler.HandleGitea)
	})

	// Write timeout for non-streaming routes. This replaces the global
	// http.Server WriteTimeout (which must be 0 to allow SSE streams).
	writeTimeout := 15 * time.Second

	// UI Routes (Require CSRF protection)
	r.Group(func(r chi.Router) {
		r.Use(auth.CSRFMiddleware)

		// Serve static files (no timeout — static file server manages its own I/O)
		fileServer := http.FileServer(http.Dir("static"))
		r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

		// Health check endpoints (lightweight, but include timeout for consistency)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(writeTimeout))
			r.Get("/health", healthHandler.Health)
			r.Get("/health/ready", healthHandler.HealthReady)
		})

		// Public routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(writeTimeout))
			r.Get("/", handlers.Index)
			r.Get("/login", authHandler.LoginPage)
			r.Post("/auth/login", authHandler.Login)
			r.Get("/auth/register", authHandler.RegisterPage)
			r.Post("/auth/register", authHandler.Register)
			r.Get("/auth/github", authHandler.GitHubLogin)
			r.Get("/auth/github/callback", authHandler.GitHubCallback)
			r.Post("/auth/logout", authHandler.Logout)
		})

		// Authenticated routes
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)

			// --- Standard request/response routes (with timeout) ---
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(writeTimeout))

				r.Get("/dashboard", dashboardHandler.Dashboard)
				r.Get("/dashboard/stats", dashboardHandler.DashboardStats)
				r.Post("/dashboard/prune", dashboardHandler.Prune)

				// Git API
				r.Route("/api/git", func(r chi.Router) {
					// Query-param based routes for HTMX components
					r.Get("/repos", gitHandler.ListRepositories)
					r.Get("/branches", gitHandler.ListBranches)
					// Path-param based routes (legacy/direct API)
					r.Get("/sources/{sourceId}/repos", gitHandler.ListRepositories)
					r.Get("/sources/{sourceId}/repos/{owner}/{repo}/branches", gitHandler.ListBranches)
					r.Get("/sources/{sourceId}/repos/{owner}/{repo}/tags", gitHandler.ListTags)
				})

				r.Route("/projects", func(r chi.Router) {
					r.Get("/", projectHandler.ListProjects)
					r.Get("/new", projectHandler.NewProjectPage)
					r.Post("/", projectHandler.CreateProject)
					r.Route("/{id}", func(r chi.Router) {
						r.Get("/", projectHandler.GetProject)
						r.Put("/", projectHandler.UpdateProject)
						r.Put("/settings", projectHandler.UpdateSettings)
						r.Delete("/", projectHandler.DeleteProject)
						r.Get("/deployments", projectHandler.GetProject)
						r.Get("/settings", projectHandler.GetProject)
						r.Get("/services", projectHandler.GetProject)
						r.Post("/services", projectHandler.LinkService)
						r.Delete("/services/{serviceId}", projectHandler.UnlinkService)

						// Deployments (non-streaming)
						r.Post("/deployments", deploymentHandler.TriggerDeployment)
						r.Route("/deployments/{deploymentID}", func(r chi.Router) {
							r.Get("/", deploymentHandler.GetDeployment)
							r.Get("/status", deploymentHandler.GetDeploymentStatus)
							r.Post("/rollback", deploymentHandler.RollbackDeployment)
							r.Post("/cancel", deploymentHandler.CancelDeployment)
						})

						// Environments
						r.Route("/envs", func(r chi.Router) {
							r.Get("/", envHandler.ListEnvironments)
							r.Post("/", envHandler.CreateEnvironment)
							r.Put("/{envId}", envHandler.UpdateEnvironment)
							r.Get("/new", envHandler.NewEnvironmentModal)
							r.Post("/{envId}/variables/sync", envHandler.SyncEnvFromRepo)
						})
					})
				})

				// Global environment variable routes
				r.Route("/envs/{envId}", func(r chi.Router) {
					r.Route("/variables", func(r chi.Router) {
						r.Get("/", envHandler.GetVariables)
						r.Post("/", envHandler.AddVariable)
						r.Get("/new", envHandler.NewVariableModal)
						r.Delete("/{varId}", envHandler.DeleteVariable)
					})
				})

				// Service Catalog
				r.Route("/services", func(r chi.Router) {
					r.Get("/", serviceHandler.Catalog)
					r.Post("/", serviceHandler.Deploy) // Fallback if specific template route isn't used
					r.Get("/{template}/configure", serviceHandler.Configure)
					r.Post("/{template}", serviceHandler.Deploy)

					// Instance management
					r.Route("/{id:[a-f0-9-]{36}}", func(r chi.Router) {
						r.Get("/", serviceHandler.Show)
						r.Delete("/", serviceHandler.Delete)
						r.Post("/restart", serviceHandler.Restart)
					})
				})

				// Settings
				r.Route("/settings", func(r chi.Router) {
					r.Get("/profile", settingsHandler.Profile)
					r.Put("/profile", settingsHandler.UpdateProfile)
					r.Put("/password", settingsHandler.UpdatePassword)
					r.Post("/prune", settingsHandler.PruneSystem)
					r.Route("/tokens", func(r chi.Router) {
						r.Get("/", settingsHandler.ListAPITokens)
						r.Get("/new", settingsHandler.NewAPITokenModal)
						r.Post("/", settingsHandler.CreateAPIToken)
						r.Delete("/{id}", settingsHandler.DeleteAPIToken)
					})
					r.Route("/git", func(r chi.Router) {
						r.Get("/", settingsHandler.GitSources)
						r.Post("/", settingsHandler.CreateGitSource)
						r.Get("/new", settingsHandler.NewGitSourceModal)
						r.Delete("/{id}", settingsHandler.DeleteGitSource)
					})
				})

				// Containers
				r.Route("/containers", func(r chi.Router) {
					r.Get("/", containerHandler.List)
					r.Get("/api", containerHandler.ListAPI)
					r.Get("/{id}", containerHandler.Detail)
					r.Post("/{id}/stop", containerHandler.Stop)
					r.Post("/{id}/restart", containerHandler.Restart)
				})
			})

			// --- SSE streaming routes (NO timeout) ---
			r.Route("/projects/{id}/deployments/{deploymentID}", func(r chi.Router) {
				r.Get("/logs", sseHandler.DeploymentLogs)
			})
			r.Route("/containers/{id}", func(r chi.Router) {
				r.Get("/stats", sseHandler.ContainerStats)
				r.Get("/logs", sseHandler.ContainerLogs)
			})
		})
	})

	return r
}
