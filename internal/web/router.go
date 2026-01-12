// Package web provides HTTP routing and handlers.
package web

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/user/gocool/internal/config"
	"github.com/user/gocool/internal/web/handlers"
)

// NewRouter creates and configures the HTTP router.
func NewRouter(cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Serve static files
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health check endpoints
	r.Get("/health", handlers.Health)
	r.Get("/health/ready", handlers.HealthReady)

	// Public routes
	r.Group(func(r chi.Router) {
		r.Get("/", handlers.Index)
		r.Get("/login", handlers.LoginPage)
	})

	// TODO: Add authenticated routes
	// r.Group(func(r chi.Router) {
	// 	r.Use(authMiddleware)
	// 	r.Get("/dashboard", handlers.Dashboard)
	// 	r.Route("/projects", func(r chi.Router) {
	// 		r.Get("/", handlers.ListProjects)
	// 		r.Post("/", handlers.CreateProject)
	// 	})
	// })

	return r
}
