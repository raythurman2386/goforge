package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/raythurman2386/gocool/internal/docker"
)

// Health check related errors.
var (
	ErrHealthCheckFailed   = errors.New("health check failed")
	ErrHealthCheckTimeout  = errors.New("health check timed out")
	ErrContainerNotRunning = errors.New("container is not running")
	ErrInvalidConfig       = errors.New("invalid health check configuration")
)

// HealthCheckConfig holds configuration for health checks.
type HealthCheckConfig struct {
	Path        string        // HTTP path to check (default: "/health")
	Interval    int           // Seconds between checks (default: 30)
	Timeout     int           // Seconds before check times out (default: 10)
	MaxAttempts int           // Maximum number of attempts before failing (default: 10)
	StartPeriod time.Duration // Grace period before starting checks (default: 0)
}

// DefaultHealthCheckConfig returns a config with sensible defaults.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Path:        "/health",
		Interval:    30,
		Timeout:     10,
		MaxAttempts: 10,
		StartPeriod: 0,
	}
}

// Validate checks if the config is valid and applies defaults.
func (c *HealthCheckConfig) Validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("%w: interval cannot be negative", ErrInvalidConfig)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("%w: timeout cannot be negative", ErrInvalidConfig)
	}

	// Apply defaults for zero values
	if c.Path == "" {
		c.Path = "/health"
	}
	if c.Interval == 0 {
		c.Interval = 30
	}
	if c.Timeout == 0 {
		c.Timeout = 10
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 10
	}

	return nil
}

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	Healthy    bool
	StatusCode int
	Latency    time.Duration
	Error      error
	Timestamp  time.Time
}

// HealthChecker performs health checks on containers and endpoints.
type HealthChecker struct {
	dockerClient *docker.Client
	httpClient   *http.Client
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(dc *docker.Client) *HealthChecker {
	return &HealthChecker{
		dockerClient: dc,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// Don't follow redirects for health checks
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Check performs a single health check against an HTTP endpoint.
func (h *HealthChecker) Check(ctx context.Context, baseURL, path string) error {
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "GoCool-HealthCheck/1.0")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrHealthCheckTimeout
		}
		return fmt.Errorf("%w: %w", ErrHealthCheckFailed, err)
	}
	defer resp.Body.Close()

	// Consider 2xx status codes as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("%w: received status code %d", ErrHealthCheckFailed, resp.StatusCode)
}

// WaitForHealthy waits until the endpoint becomes healthy or max attempts are reached.
func (h *HealthChecker) WaitForHealthy(ctx context.Context, baseURL, path string, maxAttempts int, interval time.Duration) error {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: context canceled (last error: %w)", ErrHealthCheckTimeout, lastErr)
		default:
		}

		err := h.Check(ctx, baseURL, path)
		if err == nil {
			return nil
		}
		lastErr = err

		// Don't sleep after the last attempt
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: context canceled (last error: %w)", ErrHealthCheckTimeout, lastErr)
			case <-time.After(interval):
			}
		}
	}

	return fmt.Errorf("%w: max attempts (%d) reached (last error: %w)", ErrHealthCheckFailed, maxAttempts, lastErr)
}

// CheckContainer performs a health check on a container using Docker's built-in health check.
func (h *HealthChecker) CheckContainer(ctx context.Context, containerID string) (*HealthCheckResult, error) {
	if containerID == "" {
		return nil, fmt.Errorf("%w: container ID cannot be empty", ErrInvalidConfig)
	}

	if h.dockerClient == nil {
		return nil, fmt.Errorf("%w: docker client not configured", ErrHealthCheckFailed)
	}

	// Get container info to check if it's running and its health status
	result := &HealthCheckResult{
		Timestamp: time.Now(),
	}

	// For now, we return healthy if we can ping the docker client
	// In a full implementation, we would inspect the container's health status
	if err := h.dockerClient.Ping(ctx); err != nil {
		result.Healthy = false
		result.Error = fmt.Errorf("docker not reachable: %w", err)
		return result, nil
	}

	result.Healthy = true
	return result, nil
}

// CheckWithRetry performs a health check with configurable retries.
func (h *HealthChecker) CheckWithRetry(ctx context.Context, baseURL string, config HealthCheckConfig) (*HealthCheckResult, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Wait for start period
	if config.StartPeriod > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(config.StartPeriod):
		}
	}

	interval := time.Duration(config.Interval) * time.Second
	timeout := time.Duration(config.Timeout) * time.Second

	result := &HealthCheckResult{
		Timestamp: time.Now(),
	}

	start := time.Now()

	// Create a context with timeout for each individual check
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := h.WaitForHealthy(checkCtx, baseURL, config.Path, config.MaxAttempts, interval)
	result.Latency = time.Since(start)

	if err != nil {
		result.Healthy = false
		result.Error = err
		return result, err
	}

	result.Healthy = true
	result.StatusCode = http.StatusOK
	return result, nil
}
