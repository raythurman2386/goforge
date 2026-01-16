// Package web provides HTTP routing and handlers.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/web/handlers"
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
) http.Handler {
	r := chi.NewRouter()

	// Middleware stack
	// Session middleware is safe for all routes (just populates context)
	r.Use(auth.Middleware(sessionManager))

	// Webhooks (No CSRF protection needed/wanted as they are machine-to-machine)
	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/github", webhookHandler.HandleGitHub)
		r.Post("/gitlab", webhookHandler.HandleGitLab)
		r.Post("/gitea", webhookHandler.HandleGitea)
	})

	// UI Routes (Require CSRF protection)
	r.Group(func(r chi.Router) {
		r.Use(auth.CSRFMiddleware)

		// Serve static files
		fileServer := http.FileServer(http.Dir("static"))
		r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

		// Health check endpoints
		r.Get("/health", handlers.Health)
		r.Get("/health/ready", handlers.HealthReady)

		// Public routes
		r.Group(func(r chi.Router) {
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
			r.Get("/dashboard", dashboardHandler.Dashboard)

			// Git API
			r.Route("/api/git", func(r chi.Router) {
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
					r.Delete("/", projectHandler.DeleteProject)
					r.Get("/deployments", projectHandler.GetProject)
					r.Get("/settings", projectHandler.GetProject)

					// Deployments
					r.Post("/deployments", deploymentHandler.TriggerDeployment)
					r.Route("/deployments/{deploymentID}", func(r chi.Router) {
						r.Get("/", deploymentHandler.GetDeployment)
						r.Post("/rollback", deploymentHandler.RollbackDeployment)
						r.Post("/cancel", deploymentHandler.CancelDeployment)
						r.Get("/logs", sseHandler.DeploymentLogs)
					})

					// Environments
					r.Route("/envs", func(r chi.Router) {
						r.Get("/", envHandler.ListEnvironments)
						r.Post("/", envHandler.CreateEnvironment)
						r.Get("/new", envHandler.NewEnvironmentModal)
					})
				})
			})

			// Global environment variable routes
			r.Route("/envs/{envId}", func(r chi.Router) {
				r.Route("/variables", func(r chi.Router) {
					r.Get("/", envHandler.GetVariables)
					r.Post("/", envHandler.AddVariable)
					r.Get("/new", envHandler.NewVariableModal)
				})
			})

			// Service Catalog
			r.Route("/services", func(r chi.Router) {
				r.Get("/", serviceHandler.Catalog)
				r.Post("/", serviceHandler.Deploy) // Fallback if specific template route isn't used
				r.Get("/{template}/configure", serviceHandler.Configure)
				r.Post("/{template}", serviceHandler.Deploy)

				// Instance management
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", serviceHandler.Show)
					r.Delete("/", serviceHandler.Delete)
					r.Post("/restart", serviceHandler.Restart)
				})
			})

			// Containers
			r.Route("/containers/{id}", func(r chi.Router) {
				r.Get("/stats", sseHandler.ContainerStats)
				r.Get("/logs", sseHandler.ContainerLogs)
			})

			// Settings
			r.Route("/settings", func(r chi.Router) {
				r.Route("/git", func(r chi.Router) {
					r.Get("/", settingsHandler.GitSources)
					r.Post("/", settingsHandler.CreateGitSource)
					r.Get("/new", settingsHandler.NewGitSourceModal)
					r.Delete("/{id}", settingsHandler.DeleteGitSource)
				})
			})
		})
	})

	return r
}
