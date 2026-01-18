// Package proxy provides proxy configuration and label generation.
package proxy

import (
	"fmt"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
)

const (
	labelTrue = "true"

	// Traefik entrypoint names.
	entrypointHTTP  = "web"
	entrypointHTTPS = "websecure"
)

// Labels represents Traefik Docker labels.
type Labels map[string]string

// Merge combines another Labels map into this one, overwriting existing keys.
func (l Labels) Merge(other Labels) {
	for k, v := range other {
		l[k] = v
	}
}

// ToMap returns a copy of the labels as a standard map.
func (l Labels) ToMap() map[string]string {
	m := make(map[string]string, len(l))
	for k, v := range l {
		m[k] = v
	}
	return m
}

// LabelOptions contains configuration options for generating Traefik labels.
type LabelOptions struct {
	// CloudflareProxyMode indicates traffic comes through Cloudflare Tunnel.
	// When true, uses HTTP entrypoint and skips TLS/cert configuration
	// since Cloudflare handles SSL termination at the edge.
	CloudflareProxyMode bool

	// EnableHTTPRedirect adds HTTP to HTTPS redirect middleware.
	// Ignored when CloudflareProxyMode is true.
	EnableHTTPRedirect bool

	// PathPrefix adds path-based routing rule (e.g., "/api/v1").
	PathPrefix string

	// StripPrefix removes the path prefix before forwarding to backend.
	StripPrefix bool

	// Priority sets the router priority (higher = matched first).
	Priority int

	// RateLimit configures request rate limiting.
	RateLimit *RateLimitConfig

	// Headers configures security headers middleware.
	Headers *HeadersConfig

	// HealthCheck configures load balancer health checks.
	HealthCheck *HealthCheckConfig

	// BasicAuth configures basic authentication.
	BasicAuth *BasicAuthConfig

	// IPWhitelist restricts access to specific IP ranges.
	IPWhitelist []string

	// CustomMiddlewares adds additional middleware references.
	CustomMiddlewares []string

	// Sticky configures sticky sessions for load balancing.
	Sticky *StickyConfig

	// CertResolver overrides the default certificate resolver.
	CertResolver string
}

// RateLimitConfig configures rate limiting middleware.
type RateLimitConfig struct {
	Average int // Average rate of requests per second.
	Burst   int // Maximum burst size.
}

// HeadersConfig configures security headers middleware.
type HeadersConfig struct {
	STSSeconds           int  // Strict-Transport-Security max-age.
	STSIncludeSubdomains bool // Include subdomains in STS.
	STSPreload           bool // Enable STS preload.
	ForceSTSHeader       bool // Force STS header even on HTTP.
	ContentTypeNosniff   bool // X-Content-Type-Options: nosniff.
	BrowserXSSFilter     bool // X-XSS-Protection: 1; mode=block.
	FrameDeny            bool // X-Frame-Options: DENY.
}

// HealthCheckConfig configures load balancer health checks.
type HealthCheckConfig struct {
	Path     string // Health check endpoint path.
	Interval string // Check interval (e.g., "10s").
	Timeout  string // Check timeout (e.g., "3s").
}

// BasicAuthConfig configures basic authentication.
type BasicAuthConfig struct {
	Users []string // List of user:password-hash entries.
}

// StickyConfig configures sticky session cookies.
type StickyConfig struct {
	CookieName string // Cookie name for sticky sessions.
	Secure     bool   // Cookie secure flag.
	HTTPOnly   bool   // Cookie httpOnly flag.
}

// GenerateName creates a unique router/service name from project and environment.
func GenerateName(projectID, envName string) string {
	return fmt.Sprintf("goforge-%s-%s", projectID, envName)
}

// GenerateLabels generates Traefik labels for an environment with default options.
func GenerateLabels(p *models.Project, env *models.Environment, network string) Labels {
	return GenerateLabelsWithOptions(p, env, network, LabelOptions{})
}

// GenerateLabelsForCloudflare generates Traefik labels optimized for Cloudflare Tunnel.
// When cloudflareMode is true, labels are configured for HTTP entrypoint since
// Cloudflare handles SSL termination at the edge.
func GenerateLabelsForCloudflare(p *models.Project, env *models.Environment, network string, cloudflareMode bool) Labels {
	return GenerateLabelsWithOptions(p, env, network, LabelOptions{
		CloudflareProxyMode: cloudflareMode,
	})
}

// GenerateLabelsWithOptions generates Traefik labels with custom options.
func GenerateLabelsWithOptions(p *models.Project, env *models.Environment, network string, opts LabelOptions) Labels {
	name := GenerateName(p.ID, env.Name)

	labels := Labels{
		"traefik.enable":         labelTrue,
		"traefik.docker.network": network,
		"goforge.managed":        labelTrue,
		"goforge.project.id":     p.ID,
		"goforge.environment.id": env.ID,
	}

	// Determine domain: Environment domain takes precedence, then Project Custom Domain
	domain := ""
	if env.Domain.Valid && env.Domain.String != "" {
		domain = env.Domain.String
	} else if p.CustomDomain.Valid && p.CustomDomain.String != "" {
		domain = p.CustomDomain.String
	}

	// Only add routing labels if domain is set
	if domain == "" {
		return labels
	}

	// Build the routing rule
	rule := buildRoutingRule(domain, opts.PathPrefix)

	// Configure router based on proxy mode
	if opts.CloudflareProxyMode {
		// Cloudflare Tunnel mode: traffic arrives as HTTP after Cloudflare terminates SSL
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", name)] = rule
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", name)] = entrypointHTTP
		// No TLS config needed - Cloudflare handles SSL at the edge
	} else {
		// Standard mode: Traefik handles TLS with Let's Encrypt
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", name)] = rule
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", name)] = entrypointHTTPS

		certResolver := "letsencrypt"
		if opts.CertResolver != "" {
			certResolver = opts.CertResolver
		}
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = certResolver
	}

	// Service port configuration
	port := 80
	if env.Port.Valid {
		port = int(env.Port.Int32)
	}
	labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name)] = fmt.Sprintf("%d", port)

	// Router priority
	if opts.Priority > 0 {
		labels[fmt.Sprintf("traefik.http.routers.%s.priority", name)] = fmt.Sprintf("%d", opts.Priority)
	}

	// Collect middlewares for the main router
	var middlewares []string

	// HTTP to HTTPS redirect (skip in Cloudflare mode - SSL is handled at edge)
	if opts.EnableHTTPRedirect && !opts.CloudflareProxyMode {
		addHTTPRedirectLabels(labels, name, domain, opts.PathPrefix)
	}

	// Rate limiting middleware
	if opts.RateLimit != nil {
		middlewareName := name + "-ratelimit"
		addRateLimitLabels(labels, name, opts.RateLimit)
		middlewares = append(middlewares, middlewareName)
	}

	// Security headers middleware
	if opts.Headers != nil {
		middlewareName := name + "-headers"
		addHeadersLabels(labels, name, opts.Headers)
		middlewares = append(middlewares, middlewareName)
	}

	// Strip prefix middleware
	if opts.StripPrefix && opts.PathPrefix != "" {
		middlewareName := name + "-stripprefix"
		addStripPrefixLabels(labels, name, opts.PathPrefix)
		middlewares = append(middlewares, middlewareName)
	}

	// Basic auth middleware
	if opts.BasicAuth != nil && len(opts.BasicAuth.Users) > 0 {
		middlewareName := name + "-basicauth"
		addBasicAuthLabels(labels, name, opts.BasicAuth)
		middlewares = append(middlewares, middlewareName)
	}

	// IP whitelist middleware
	if len(opts.IPWhitelist) > 0 {
		middlewareName := name + "-ipwhitelist"
		addIPWhitelistLabels(labels, name, opts.IPWhitelist)
		middlewares = append(middlewares, middlewareName)
	}

	// Add custom middlewares
	middlewares = append(middlewares, opts.CustomMiddlewares...)

	// Set middlewares on main router
	if len(middlewares) > 0 {
		labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", name)] = strings.Join(middlewares, ",")
	}

	// Health check configuration
	if opts.HealthCheck != nil {
		addHealthCheckLabels(labels, name, opts.HealthCheck)
	}

	// Sticky sessions
	if opts.Sticky != nil {
		addStickyLabels(labels, name, opts.Sticky)
	}

	return labels
}

// buildRoutingRule creates the Traefik routing rule.
func buildRoutingRule(domain, pathPrefix string) string {
	rule := fmt.Sprintf("Host(`%s`)", domain)
	if pathPrefix != "" {
		rule = fmt.Sprintf("%s && PathPrefix(`%s`)", rule, pathPrefix)
	}
	return rule
}

// addHTTPRedirectLabels adds HTTP to HTTPS redirect router and middleware.
func addHTTPRedirectLabels(labels Labels, name, domain, pathPrefix string) {
	httpRouterName := name + "-http"
	redirectMiddleware := name + "-redirect"

	rule := buildRoutingRule(domain, pathPrefix)

	// HTTP router that redirects to HTTPS
	labels[fmt.Sprintf("traefik.http.routers.%s.rule", httpRouterName)] = rule
	labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", httpRouterName)] = entrypointHTTP
	labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", httpRouterName)] = redirectMiddleware

	// Redirect scheme middleware
	labels[fmt.Sprintf("traefik.http.middlewares.%s.redirectscheme.scheme", redirectMiddleware)] = "https"
	labels[fmt.Sprintf("traefik.http.middlewares.%s.redirectscheme.permanent", redirectMiddleware)] = labelTrue
}

// addRateLimitLabels adds rate limiting middleware labels.
func addRateLimitLabels(labels Labels, name string, cfg *RateLimitConfig) {
	middlewareName := name + "-ratelimit"
	labels[fmt.Sprintf("traefik.http.middlewares.%s.ratelimit.average", middlewareName)] = fmt.Sprintf("%d", cfg.Average)
	labels[fmt.Sprintf("traefik.http.middlewares.%s.ratelimit.burst", middlewareName)] = fmt.Sprintf("%d", cfg.Burst)
}

// addHeadersLabels adds security headers middleware labels.
func addHeadersLabels(labels Labels, name string, cfg *HeadersConfig) {
	middlewareName := name + "-headers"
	prefix := fmt.Sprintf("traefik.http.middlewares.%s.headers", middlewareName)

	if cfg.STSSeconds > 0 {
		labels[prefix+".stsSeconds"] = fmt.Sprintf("%d", cfg.STSSeconds)
	}
	if cfg.STSIncludeSubdomains {
		labels[prefix+".stsIncludeSubdomains"] = labelTrue
	}
	if cfg.STSPreload {
		labels[prefix+".stsPreload"] = labelTrue
	}
	if cfg.ForceSTSHeader {
		labels[prefix+".forceSTSHeader"] = labelTrue
	}
	if cfg.ContentTypeNosniff {
		labels[prefix+".contentTypeNosniff"] = labelTrue
	}
	if cfg.BrowserXSSFilter {
		labels[prefix+".browserXssFilter"] = labelTrue
	}
	if cfg.FrameDeny {
		labels[prefix+".frameDeny"] = labelTrue
	}
}

// addStripPrefixLabels adds strip prefix middleware labels.
func addStripPrefixLabels(labels Labels, name, pathPrefix string) {
	middlewareName := name + "-stripprefix"
	labels[fmt.Sprintf("traefik.http.middlewares.%s.stripprefix.prefixes", middlewareName)] = pathPrefix
}

// addBasicAuthLabels adds basic auth middleware labels.
func addBasicAuthLabels(labels Labels, name string, cfg *BasicAuthConfig) {
	middlewareName := name + "-basicauth"
	labels[fmt.Sprintf("traefik.http.middlewares.%s.basicauth.users", middlewareName)] = strings.Join(cfg.Users, ",")
}

// addIPWhitelistLabels adds IP whitelist middleware labels.
func addIPWhitelistLabels(labels Labels, name string, ips []string) {
	middlewareName := name + "-ipwhitelist"
	labels[fmt.Sprintf("traefik.http.middlewares.%s.ipwhitelist.sourcerange", middlewareName)] = strings.Join(ips, ",")
}

// addHealthCheckLabels adds load balancer health check labels.
func addHealthCheckLabels(labels Labels, name string, cfg *HealthCheckConfig) {
	prefix := fmt.Sprintf("traefik.http.services.%s.loadbalancer.healthcheck", name)
	if cfg.Path != "" {
		labels[prefix+".path"] = cfg.Path
	}
	if cfg.Interval != "" {
		labels[prefix+".interval"] = cfg.Interval
	}
	if cfg.Timeout != "" {
		labels[prefix+".timeout"] = cfg.Timeout
	}
}

// addStickyLabels adds sticky session labels.
func addStickyLabels(labels Labels, name string, cfg *StickyConfig) {
	prefix := fmt.Sprintf("traefik.http.services.%s.loadbalancer.sticky.cookie", name)
	if cfg.CookieName != "" {
		labels[prefix+".name"] = cfg.CookieName
	}
	if cfg.Secure {
		labels[prefix+".secure"] = labelTrue
	}
	if cfg.HTTPOnly {
		labels[prefix+".httpOnly"] = labelTrue
	}
}

// DefaultSecurityHeaders returns a HeadersConfig with recommended security settings.
func DefaultSecurityHeaders() *HeadersConfig {
	return &HeadersConfig{
		STSSeconds:           31536000, // 1 year
		STSIncludeSubdomains: true,
		STSPreload:           true,
		ForceSTSHeader:       true,
		ContentTypeNosniff:   true,
		BrowserXSSFilter:     true,
		FrameDeny:            true,
	}
}

// DefaultRateLimit returns a RateLimitConfig with reasonable defaults.
func DefaultRateLimit() *RateLimitConfig {
	return &RateLimitConfig{
		Average: 100,
		Burst:   50,
	}
}
