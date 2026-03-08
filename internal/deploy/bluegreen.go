package deploy

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
)

// BlueGreenConfig holds configuration for blue-green deployment.
type BlueGreenConfig struct {
	HealthCheckCount    int           // Number of successful checks required (default: 3)
	HealthCheckInterval time.Duration // Interval between health checks (default: 10s)
	GracePeriod         time.Duration // Time to keep old container after switch (default: 5m)
	HealthCheckTimeout  time.Duration // Timeout for each health check (default: 10s)
}

// DefaultBlueGreenConfig returns a config with values per SPECIFICATION.md section 7.
func DefaultBlueGreenConfig() BlueGreenConfig {
	return BlueGreenConfig{
		HealthCheckCount:    3,
		HealthCheckInterval: 10 * time.Second,
		GracePeriod:         5 * time.Minute,
		HealthCheckTimeout:  10 * time.Second,
	}
}

// Validate checks if the config is valid.
func (c *BlueGreenConfig) Validate() error {
	if c.HealthCheckCount <= 0 {
		return fmt.Errorf("%w: health check count must be positive", ErrInvalidConfig)
	}
	if c.HealthCheckInterval < 0 {
		return fmt.Errorf("%w: health check interval cannot be negative", ErrInvalidConfig)
	}
	if c.GracePeriod < 0 {
		return fmt.Errorf("%w: grace period cannot be negative", ErrInvalidConfig)
	}

	// Apply defaults for zero values
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 10 * time.Second
	}
	if c.GracePeriod == 0 {
		c.GracePeriod = 5 * time.Minute
	}
	if c.HealthCheckTimeout == 0 {
		c.HealthCheckTimeout = 10 * time.Second
	}

	return nil
}

// BlueGreenRequest contains the parameters for a blue-green deployment.
type BlueGreenRequest struct {
	DeploymentID    string
	NewContainerID  string
	OldContainerID  string // Empty for first deployment
	HealthCheckURL  string
	HealthCheckPath string
}

// BlueGreenResult contains the result of a blue-green deployment.
type BlueGreenResult struct {
	Success         bool
	ActiveContainer string
	RolledBack      bool
	Error           error
}

// BlueGreenDeployer handles blue-green deployment strategy.
type BlueGreenDeployer struct {
	dockerClient  *docker.Client
	httpClient    *http.Client
	config        BlueGreenConfig
	cleanupWg     sync.WaitGroup
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
}

// NewBlueGreenDeployer creates a new blue-green deployer.
func NewBlueGreenDeployer(dc *docker.Client, hc *http.Client, config BlueGreenConfig) *BlueGreenDeployer {
	if hc == nil {
		hc = &http.Client{
			Timeout: config.HealthCheckTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &BlueGreenDeployer{
		dockerClient:  dc,
		httpClient:    hc,
		config:        config,
		cleanupCtx:    ctx,
		cleanupCancel: cancel,
	}
}

// Stop gracefully shuts down the deployer, waiting for all cleanup goroutines to complete.
func (d *BlueGreenDeployer) Stop() {
	d.cleanupCancel()
	d.cleanupWg.Wait()
}

// Deploy performs a blue-green deployment with health check verification.
func (d *BlueGreenDeployer) Deploy(ctx context.Context, req BlueGreenRequest) (*BlueGreenResult, error) {
	result := &BlueGreenResult{
		ActiveContainer: req.OldContainerID,
	}

	// Validate config
	if err := d.config.Validate(); err != nil {
		result.Error = err
		return result, err
	}

	// Check context before starting
	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		return result, ctx.Err()
	default:
	}

	// 1. Verify new container health with required number of successful checks
	successCount := 0

	for attempt := 0; attempt < d.config.HealthCheckCount*3 && successCount < d.config.HealthCheckCount; attempt++ {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			return result, ctx.Err()
		default:
		}

		err := d.healthCheck(ctx, req.HealthCheckURL, req.HealthCheckPath)
		if err == nil {
			successCount++
		} else {
			successCount = 0 // Reset on failure - require consecutive successes
		}

		if successCount < d.config.HealthCheckCount {
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				return result, ctx.Err()
			case <-time.After(d.config.HealthCheckInterval):
			}
		}
	}

	if successCount < d.config.HealthCheckCount {
		// Health check failed, trigger rollback
		result.RolledBack = true
		result.Error = fmt.Errorf("%w: required %d successful checks, got %d", ErrHealthCheckFailed, d.config.HealthCheckCount, successCount)
		return result, result.Error
	}

	// 2. Health check passed - switch traffic to new container
	// In a full implementation, this would update Traefik labels
	result.ActiveContainer = req.NewContainerID
	result.Success = true

	// 3. Schedule old container cleanup (in background, non-blocking)
	if req.OldContainerID != "" {
		d.cleanupWg.Add(1)
		//nolint:contextcheck // Intentionally using stored context as cleanup runs after deployment completes
		go d.scheduleCleanup(d.cleanupCtx, req.OldContainerID, d.config.GracePeriod)
	}

	return result, nil
}

// healthCheck performs a single HTTP health check.
func (d *BlueGreenDeployer) healthCheck(ctx context.Context, baseURL, path string) error {
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "GoForge-BlueGreen/1.0")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHealthCheckFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("%w: received status code %d", ErrHealthCheckFailed, resp.StatusCode)
}

// scheduleCleanup schedules the removal of the old container after the grace period.
// It listens for cancellation on the provided context to allow graceful shutdown.
func (d *BlueGreenDeployer) scheduleCleanup(ctx context.Context, containerID string, gracePeriod time.Duration) {
	defer d.cleanupWg.Done()

	select {
	case <-ctx.Done():
		// Context canceled, abort cleanup
		return
	case <-time.After(gracePeriod):
		// Grace period elapsed, proceed with cleanup
	}

	if d.dockerClient != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Best effort cleanup - stop then remove. Don't fail if container is already gone.
		//nolint:errcheck,contextcheck // Intentional: best effort cleanup with independent timeout context
		_ = d.dockerClient.StopContainer(cleanupCtx, containerID)
		//nolint:errcheck,contextcheck // Intentional: best effort removal with independent timeout context
		_ = d.dockerClient.RemoveContainer(cleanupCtx, containerID)
	}
}
