// Package web provides HTTP routing and handlers.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/web/handlers"
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
) http.Handler {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(auth.CSRFMiddleware)
	r.Use(auth.Middleware(sessionManager))

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

	return r
}
