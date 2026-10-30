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
	apiTokenRepo auth.TokenLookup,
	userRepo auth.UserLookup,
	rateLimiter *middleware.RateLimiter,
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
	apiV1 http.Handler,
	mcpHTTP http.Handler,
) http.Handler {
	r := chi.NewRouter()

	// Client IP trust for getClientIP: TRUSTED_PROXIES (default private/loopback);
	// CF-Connecting-IP only with CLOUDFLARE_PROXY_MODE.
	handlers.SetProxyTrust(httputil.NewProxyTrust(cfg.TrustedProxies, cfg.CloudflareProxyMode))

	// Middleware stack
	r.Use(chi_middleware.Recoverer)
	// Browser hardening headers (CSP frame-ancestors, XFO, nosniff,
	// Referrer-Policy, HSTS on HTTPS) on every response.
	r.Use(middleware.SecurityHeaders)
	// Rate limiting
	r.Use(rateLimiter.Middleware)

	// Session middleware just populates context. API token middleware
	// authenticates Authorization: Bearer headers with legacy full-scope
	// tokens; it runs after the session middleware so session auth wins.
	// Neither runs on /api/v1 or /mcp, which authenticate bearer tokens
	// themselves and ignore cookies.
	sessionAuth := func(next http.Handler) http.Handler {
		return auth.Middleware(sessionManager)(auth.APITokenMiddleware(apiTokenRepo, userRepo)(next))
	}

	// Global error handlers
	r.NotFound(sessionAuth(http.HandlerFunc(handlers.NotFoundHandler)).ServeHTTP)
	r.MethodNotAllowed(sessionAuth(http.HandlerFunc(handlers.MethodNotAllowedHandler)).ServeHTTP)

	// Write timeout for non-streaming routes. This replaces the global
	// http.Server WriteTimeout (which must be 0 to allow SSE streams).
	writeTimeout := 15 * time.Second

	// JSON API v1: bearer token only, scope-checked, outside the CSRF group.
	if apiV1 != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(writeTimeout))
			r.Mount("/api/v1", apiV1)
		})
	}

	// MCP over streamable HTTP: the same bearer tokens and gates as
	// /api/v1, outside the session and CSRF groups. nil when
	// GOFORGE_MCP_HTTP=false.
	if mcpHTTP != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(writeTimeout))
			r.Handle("/mcp", mcpHTTP)
		})
	}

	r.Group(func(r chi.Router) {
		r.Use(sessionAuth)

		// Webhooks (No CSRF protection needed/wanted as they are machine-to-machine)
		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/github", webhookHandler.HandleGitHub)
			r.Post("/gitlab", webhookHandler.HandleGitLab)
			r.Post("/gitea", webhookHandler.HandleGitea)
		})

		// UI Routes (Require CSRF protection)
		r.Group(func(r chi.Router) {
			r.Use(auth.NewCSRF(cfg.CSRFSecret).Middleware)

			// Serve static files (no timeout — static file server manages its own I/O)
			fileServer := http.FileServer(http.Dir("static"))
			r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
			// Browsers request /favicon.ico regardless of <link rel="icon">.
			r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, "static/favicon.ico")
			})

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
				// UI kit gallery: development only, never routed in production.
				if cfg.DevMode {
					r.Get("/dev/ui", handlers.UIGallery)
				}
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

							// Deployments (non-streaming). Detail routes live with the SSE
							// mount below. A second mount of the same pattern 404s.
							r.Post("/deployments", deploymentHandler.TriggerDeployment)

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
						r.Get("/instances", serviceHandler.InstanceList)
						r.Post("/", serviceHandler.Deploy) // Fallback if specific template route isn't used
						r.Get("/{template}/configure", serviceHandler.Configure)
						r.Post("/{template}", serviceHandler.Deploy)

						// Instance management
						r.Route("/{id:[a-f0-9-]{36}}", func(r chi.Router) {
							r.Get("/", serviceHandler.Show)
							r.Delete("/", serviceHandler.Delete)
							r.Post("/restart", serviceHandler.Restart)
							r.Get("/secret", serviceHandler.RevealSecret)
							r.Get("/edit", serviceHandler.EditConfig)
							r.Post("/edit", serviceHandler.UpdateConfig)
							r.Post("/update", serviceHandler.UpdateImage)
						})
					})

					// Settings
					r.Route("/settings", func(r chi.Router) {
						r.Get("/profile", settingsHandler.Profile)
						r.Put("/profile", settingsHandler.UpdateProfile)
						r.Put("/password", settingsHandler.UpdatePassword)
						r.Post("/prune", settingsHandler.PruneSystem)
						r.Put("/backups", settingsHandler.UpdateBackupSettings)
						r.Post("/backups/test", settingsHandler.TestBackupConnection)
						r.Get("/backups/restore-points", settingsHandler.RestorePoints)
						r.Route("/tokens", func(r chi.Router) {
							r.Get("/", settingsHandler.ListAPITokens)
							r.Get("/new", settingsHandler.NewAPITokenModal)
							r.Post("/", settingsHandler.CreateAPIToken)
							r.Post("/{id}/rotate", settingsHandler.RotateAPIToken)
							r.Delete("/{id}", settingsHandler.DeleteAPIToken)
						})
						r.Route("/git", func(r chi.Router) {
							r.Get("/", settingsHandler.GitSources)
							r.Post("/", settingsHandler.CreateGitSource)
							r.Get("/new", settingsHandler.NewGitSourceModal)
							r.Delete("/{id}", settingsHandler.DeleteGitSource)
						})
					})

					// Containers. Per-container routes live with the SSE mount below.
					// Register flat routes, not a /containers subrouter: a mounted
					// subrouter is a catch-all, and chi ranks catch-alls below the
					// /containers/{id} param, so /containers/api would hit Detail.
					r.Get("/containers", containerHandler.List)
					r.Get("/containers/api", containerHandler.ListAPI)
				})

				// One mount per pattern. Streaming routes stay outside the write timeout.
				// Page routes on the same pattern take the timeout individually.
				r.Route("/projects/{id}/deployments/{deploymentID}", func(r chi.Router) {
					r.With(middleware.Timeout(writeTimeout)).Get("/", deploymentHandler.GetDeployment)
					r.With(middleware.Timeout(writeTimeout)).Get("/status", deploymentHandler.GetDeploymentStatus)
					r.With(middleware.Timeout(writeTimeout)).Post("/rollback", deploymentHandler.RollbackDeployment)
					r.With(middleware.Timeout(writeTimeout)).Post("/cancel", deploymentHandler.CancelDeployment)
					r.Get("/logs", sseHandler.DeploymentLogs)
				})
				r.Route("/containers/{id}", func(r chi.Router) {
					r.With(middleware.Timeout(writeTimeout)).Get("/", containerHandler.Detail)
					r.With(middleware.Timeout(writeTimeout)).Post("/stop", containerHandler.Stop)
					r.With(middleware.Timeout(writeTimeout)).Post("/restart", containerHandler.Restart)
					r.Get("/stats", sseHandler.ContainerStats)
					r.Get("/logs", sseHandler.ContainerLogs)
				})
			})
		})
	})

	return r
}
