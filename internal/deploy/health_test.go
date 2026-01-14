package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthChecker_Check(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		delay      time.Duration
		timeout    time.Duration
		wantErr    bool
	}{
		{
			name:       "healthy endpoint returns 200",
			statusCode: http.StatusOK,
			timeout:    time.Second,
			wantErr:    false,
		},
		{
			name:       "healthy endpoint returns 204",
			statusCode: http.StatusNoContent,
			timeout:    time.Second,
			wantErr:    false,
		},
		{
			name:       "unhealthy endpoint returns 500",
			statusCode: http.StatusInternalServerError,
			timeout:    time.Second,
			wantErr:    true,
		},
		{
			name:       "unhealthy endpoint returns 503",
			statusCode: http.StatusServiceUnavailable,
			timeout:    time.Second,
			wantErr:    true,
		},
		{
			name:       "timeout causes failure",
			statusCode: http.StatusOK,
			delay:      2 * time.Second,
			timeout:    100 * time.Millisecond,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.delay > 0 {
					time.Sleep(tt.delay)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			hc := NewHealthChecker(nil)
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			err := hc.Check(ctx, server.URL, "/health")
			if (err != nil) != tt.wantErr {
				t.Errorf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHealthChecker_WaitForHealthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		attemptsToPass  int
		maxAttempts     int
		interval        time.Duration
		timeout         time.Duration
		wantErr         bool
		wantAttemptsMet bool
	}{
		{
			name:            "healthy on first attempt",
			attemptsToPass:  1,
			maxAttempts:     3,
			interval:        10 * time.Millisecond,
			timeout:         time.Second,
			wantErr:         false,
			wantAttemptsMet: true,
		},
		{
			name:            "healthy after retries",
			attemptsToPass:  3,
			maxAttempts:     5,
			interval:        10 * time.Millisecond,
			timeout:         time.Second,
			wantErr:         false,
			wantAttemptsMet: true,
		},
		{
			name:            "never becomes healthy",
			attemptsToPass:  10,
			maxAttempts:     3,
			interval:        10 * time.Millisecond,
			timeout:         time.Second,
			wantErr:         true,
			wantAttemptsMet: false,
		},
		{
			name:            "context timeout",
			attemptsToPass:  100,
			maxAttempts:     100,
			interval:        50 * time.Millisecond,
			timeout:         100 * time.Millisecond,
			wantErr:         true,
			wantAttemptsMet: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts >= tt.attemptsToPass {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()

			hc := NewHealthChecker(nil)
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			err := hc.WaitForHealthy(ctx, server.URL, "/health", tt.maxAttempts, tt.interval)
			if (err != nil) != tt.wantErr {
				t.Errorf("WaitForHealthy() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantAttemptsMet && attempts < tt.attemptsToPass {
				t.Errorf("WaitForHealthy() attempts = %d, want >= %d", attempts, tt.attemptsToPass)
			}
		})
	}
}

func TestHealthChecker_CheckContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		containerID string
		wantErr     bool
	}{
		{
			name:        "empty container ID",
			containerID: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hc := NewHealthChecker(nil)
			_, err := hc.CheckContainer(context.Background(), tt.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckContainer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHealthCheckConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  HealthCheckConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: HealthCheckConfig{
				Path:        "/health",
				Interval:    30,
				Timeout:     10,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "empty path uses default",
			config: HealthCheckConfig{
				Interval:    30,
				Timeout:     10,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "zero interval uses default",
			config: HealthCheckConfig{
				Path:        "/health",
				Timeout:     10,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "negative interval is invalid",
			config: HealthCheckConfig{
				Path:        "/health",
				Interval:    -1,
				Timeout:     10,
				MaxAttempts: 3,
			},
			wantErr: true,
		},
		{
			name: "negative timeout is invalid",
			config: HealthCheckConfig{
				Path:        "/health",
				Interval:    30,
				Timeout:     -1,
				MaxAttempts: 3,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultHealthCheckConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultHealthCheckConfig()

	if cfg.Path != "/health" {
		t.Errorf("DefaultHealthCheckConfig().Path = %q, want %q", cfg.Path, "/health")
	}

	if cfg.Interval != 30 {
		t.Errorf("DefaultHealthCheckConfig().Interval = %d, want %d", cfg.Interval, 30)
	}

	if cfg.Timeout != 10 {
		t.Errorf("DefaultHealthCheckConfig().Timeout = %d, want %d", cfg.Timeout, 10)
	}

	if cfg.MaxAttempts != 10 {
		t.Errorf("DefaultHealthCheckConfig().MaxAttempts = %d, want %d", cfg.MaxAttempts, 10)
	}
}
