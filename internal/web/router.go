// Package web provides HTTP routing and handlers.
package web

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/user/gocool/internal/auth"
	"github.com/user/gocool/internal/config"
	"github.com/user/gocool/internal/database/repositories"
	"github.com/user/gocool/internal/project"
	"github.com/user/gocool/internal/web/handlers"
)

// NewRouter creates and configures the HTTP router.
func NewRouter(cfg *config.Config, db *sql.DB) http.Handler {
	r := chi.NewRouter()

	// Initialize repositories
	userRepo := repositories.NewUserRepository(db)
	sessionRepo := repositories.NewSessionRepository(db)
	projectRepo := repositories.NewProjectRepository(db)
	environmentRepo := repositories.NewEnvironmentRepository(db)
	envVariableRepo := repositories.NewEnvVariableRepository(db)

	// Initialize services
	sessionManager := auth.NewSessionManager(sessionRepo, userRepo)
	githubProvider := auth.NewGitHubProvider(cfg)
	projectService := project.NewService(projectRepo, environmentRepo, envVariableRepo)
	authenticator := auth.NewLocalAuthenticator(userRepo)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(sessionManager, userRepo, githubProvider, authenticator)
	projectHandler := handlers.NewProjectHandler(projectService)
	envHandler := handlers.NewEnvironmentHandler(projectService)

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
		r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("Welcome to Dashboard!")) //nolint:errcheck // simple dashboard response for now
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
	})

	return r
}
