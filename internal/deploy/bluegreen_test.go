package deploy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBlueGreenConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  BlueGreenConfig
		wantErr bool
	}{
		{
			name:    "default config is valid",
			config:  DefaultBlueGreenConfig(),
			wantErr: false,
		},
		{
			name: "zero health checks is invalid",
			config: BlueGreenConfig{
				HealthCheckCount:    0,
				HealthCheckInterval: 10 * time.Second,
				GracePeriod:         5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "negative interval is invalid",
			config: BlueGreenConfig{
				HealthCheckCount:    3,
				HealthCheckInterval: -1 * time.Second,
				GracePeriod:         5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "custom valid config",
			config: BlueGreenConfig{
				HealthCheckCount:    5,
				HealthCheckInterval: 5 * time.Second,
				GracePeriod:         10 * time.Minute,
			},
			wantErr: false,
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

func TestDefaultBlueGreenConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultBlueGreenConfig()

	// Per spec: 3 successful checks, 10s interval, 5-minute grace
	if cfg.HealthCheckCount != 3 {
		t.Errorf("HealthCheckCount = %d, want 3", cfg.HealthCheckCount)
	}

	if cfg.HealthCheckInterval != 10*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 10s", cfg.HealthCheckInterval)
	}

	if cfg.GracePeriod != 5*time.Minute {
		t.Errorf("GracePeriod = %v, want 5m", cfg.GracePeriod)
	}
}

func TestBlueGreenDeployer_Deploy_Success(t *testing.T) {
	t.Parallel()

	// Mock healthy endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use fast config for tests
	config := BlueGreenConfig{
		HealthCheckCount:    3,
		HealthCheckInterval: 10 * time.Millisecond,
		GracePeriod:         1 * time.Minute,
	}
	deployer := NewBlueGreenDeployer(nil, nil, config)

	req := BlueGreenRequest{
		DeploymentID:    "deploy-123",
		NewContainerID:  "container-new",
		OldContainerID:  "container-old",
		HealthCheckURL:  server.URL,
		HealthCheckPath: "/health",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := deployer.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if !result.Success {
		t.Error("Deploy() result.Success = false, want true")
	}

	if result.ActiveContainer != req.NewContainerID {
		t.Errorf("result.ActiveContainer = %q, want %q", result.ActiveContainer, req.NewContainerID)
	}
}

func TestBlueGreenDeployer_Deploy_HealthCheckFails(t *testing.T) {
	t.Parallel()

	// Mock unhealthy endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := BlueGreenConfig{
		HealthCheckCount:    2,
		HealthCheckInterval: 10 * time.Millisecond, // Fast for tests
		GracePeriod:         1 * time.Minute,
	}
	deployer := NewBlueGreenDeployer(nil, nil, config)

	req := BlueGreenRequest{
		DeploymentID:    "deploy-456",
		NewContainerID:  "container-new",
		OldContainerID:  "container-old",
		HealthCheckURL:  server.URL,
		HealthCheckPath: "/health",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := deployer.Deploy(ctx, req)

	// Should return error and trigger rollback
	if err == nil {
		t.Fatal("Deploy() expected error when health check fails")
	}

	if !errors.Is(err, ErrHealthCheckFailed) {
		t.Errorf("Deploy() error = %v, want ErrHealthCheckFailed", err)
	}

	if result.Success {
		t.Error("result.Success = true, want false")
	}

	if !result.RolledBack {
		t.Error("result.RolledBack = false, want true (should rollback)")
	}
}

func TestBlueGreenDeployer_Deploy_EventuallyHealthy(t *testing.T) {
	t.Parallel()

	// Mock endpoint that becomes healthy after 2 attempts
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts >= 2 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	config := BlueGreenConfig{
		HealthCheckCount:    3,
		HealthCheckInterval: 10 * time.Millisecond,
		GracePeriod:         1 * time.Minute,
	}
	deployer := NewBlueGreenDeployer(nil, nil, config)

	req := BlueGreenRequest{
		DeploymentID:    "deploy-789",
		NewContainerID:  "container-new",
		OldContainerID:  "container-old",
		HealthCheckURL:  server.URL,
		HealthCheckPath: "/health",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := deployer.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Deploy() error = %v (attempts = %d)", err, attempts)
	}

	if !result.Success {
		t.Error("Deploy() result.Success = false, want true")
	}
}

func TestBlueGreenDeployer_Deploy_NoOldContainer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use fast config for tests
	config := BlueGreenConfig{
		HealthCheckCount:    3,
		HealthCheckInterval: 10 * time.Millisecond,
		GracePeriod:         1 * time.Minute,
	}
	deployer := NewBlueGreenDeployer(nil, nil, config)

	req := BlueGreenRequest{
		DeploymentID:    "deploy-first",
		NewContainerID:  "container-new",
		OldContainerID:  "", // No existing container (first deploy)
		HealthCheckURL:  server.URL,
		HealthCheckPath: "/health",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := deployer.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if !result.Success {
		t.Error("Deploy() should succeed even without old container")
	}
}

func TestBlueGreenDeployer_Deploy_ContextCanceled(t *testing.T) {
	t.Parallel()

	// Slow responding server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deployer := NewBlueGreenDeployer(nil, nil, DefaultBlueGreenConfig())

	req := BlueGreenRequest{
		DeploymentID:    "deploy-cancel",
		NewContainerID:  "container-new",
		OldContainerID:  "container-old",
		HealthCheckURL:  server.URL,
		HealthCheckPath: "/health",
	}

	// Cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deployer.Deploy(ctx, req)
	if err == nil {
		t.Error("Deploy() expected error when context is canceled")
	}
}
