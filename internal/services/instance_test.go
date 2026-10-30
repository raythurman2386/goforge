package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// mockInstanceRepository implements InstanceRepository for testing.
type mockInstanceRepository struct {
	createFunc       func(ctx context.Context, s *models.ServiceInstance) error
	getByIDFunc      func(ctx context.Context, id string) (*models.ServiceInstance, error)
	listByUserIDFunc func(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
	updateFunc       func(ctx context.Context, s *models.ServiceInstance) error
	// updateIfStatusFunc, when nil, falls back to updateFunc and always swaps.
	updateIfStatusFunc func(ctx context.Context, s *models.ServiceInstance, expected models.ServiceInstanceStatus) (bool, error)
	deleteFunc         func(ctx context.Context, id string) error
}

func (m *mockInstanceRepository) Create(ctx context.Context, s *models.ServiceInstance) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, s)
	}
	return nil
}

func (m *mockInstanceRepository) GetByID(ctx context.Context, id string) (*models.ServiceInstance, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockInstanceRepository) ListByUserID(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
	if m.listByUserIDFunc != nil {
		return m.listByUserIDFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockInstanceRepository) Update(ctx context.Context, s *models.ServiceInstance) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, s)
	}
	return nil
}

func (m *mockInstanceRepository) UpdateIfStatus(ctx context.Context, s *models.ServiceInstance, expected models.ServiceInstanceStatus) (bool, error) {
	if m.updateIfStatusFunc != nil {
		return m.updateIfStatusFunc(ctx, s, expected)
	}
	if err := m.Update(ctx, s); err != nil {
		return false, err
	}
	return true, nil
}

func (m *mockInstanceRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

// mockContainerRepository implements ContainerRepository for testing.
type mockContainerRepository struct {
	createFunc                 func(ctx context.Context, c *models.Container) error
	getByServiceInstanceIDFunc func(ctx context.Context, instanceID string) (*models.Container, error)
	deleteFunc                 func(ctx context.Context, id string) error
}

func (m *mockContainerRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockContainerRepository) Create(ctx context.Context, c *models.Container) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, c)
	}
	return nil
}

func (m *mockContainerRepository) GetByServiceInstanceID(ctx context.Context, instanceID string) (*models.Container, error) {
	if m.getByServiceInstanceIDFunc != nil {
		return m.getByServiceInstanceIDFunc(ctx, instanceID)
	}
	return nil, nil
}

// mockDockerClient implements DockerClient for testing.
type mockDockerClient struct {
	createContainerFunc  func(ctx context.Context, opts docker.ContainerOptions) (string, error)
	startContainerFunc   func(ctx context.Context, id string) error
	stopContainerFunc    func(ctx context.Context, id string) error
	removeContainerFunc  func(ctx context.Context, id string) error
	renameContainerFunc  func(ctx context.Context, id, newName string) error
	restartContainerFunc func(ctx context.Context, id string) error
	cleanupSystemFunc    func(ctx context.Context) error
	imageExistsFunc      func(ctx context.Context, image string) (bool, error)
	ensureImageFunc      func(ctx context.Context, image string) error
	pullImageFunc        func(ctx context.Context, image string) error
	waitHealthyFunc      func(ctx context.Context, id string, poll time.Duration) error
	execFunc             func(ctx context.Context, id string, opts docker.ExecOptions) (docker.ExecResult, error)
}

func (m *mockDockerClient) WaitHealthy(ctx context.Context, id string, poll time.Duration) error {
	if m.waitHealthyFunc != nil {
		return m.waitHealthyFunc(ctx, id, poll)
	}
	return nil
}

func (m *mockDockerClient) Exec(ctx context.Context, id string, opts docker.ExecOptions) (docker.ExecResult, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, id, opts)
	}
	return docker.ExecResult{}, nil
}

func (m *mockDockerClient) PullImage(ctx context.Context, image string) error {
	if m.pullImageFunc != nil {
		return m.pullImageFunc(ctx, image)
	}
	return nil
}

func (m *mockDockerClient) ImageExists(ctx context.Context, image string) (bool, error) {
	if m.imageExistsFunc != nil {
		return m.imageExistsFunc(ctx, image)
	}
	return true, nil
}

func (m *mockDockerClient) EnsureImage(ctx context.Context, image string) error {
	if m.ensureImageFunc != nil {
		return m.ensureImageFunc(ctx, image)
	}
	return nil
}

func (m *mockDockerClient) CreateContainer(ctx context.Context, opts docker.ContainerOptions) (string, error) {
	if m.createContainerFunc != nil {
		return m.createContainerFunc(ctx, opts)
	}
	return "mock-docker-id", nil
}

func (m *mockDockerClient) StartContainer(ctx context.Context, id string) error {
	if m.startContainerFunc != nil {
		return m.startContainerFunc(ctx, id)
	}
	return nil
}

func (m *mockDockerClient) StopContainer(ctx context.Context, id string) error {
	if m.stopContainerFunc != nil {
		return m.stopContainerFunc(ctx, id)
	}
	return nil
}

func (m *mockDockerClient) RemoveContainer(ctx context.Context, id string) error {
	if m.removeContainerFunc != nil {
		return m.removeContainerFunc(ctx, id)
	}
	return nil
}

func (m *mockDockerClient) RenameContainer(ctx context.Context, id, newName string) error {
	if m.renameContainerFunc != nil {
		return m.renameContainerFunc(ctx, id, newName)
	}
	return nil
}

func (m *mockDockerClient) RestartContainer(ctx context.Context, id string) error {
	if m.restartContainerFunc != nil {
		return m.restartContainerFunc(ctx, id)
	}
	return nil
}

func (m *mockDockerClient) CleanupSystem(ctx context.Context) error {
	if m.cleanupSystemFunc != nil {
		return m.cleanupSystemFunc(ctx)
	}
	return nil
}

func TestInstanceService_DeployInstance(t *testing.T) {
	t.Parallel()
	// Setup catalog
	catalog := NewCatalog("dummy")
	tmpl := &ServiceTemplate{
		Name:           "redis",
		DefaultVersion: "7.0",
	}
	catalog.templates["redis"] = tmpl

	// Setup catalog with web-facing template (has ports and health check)
	webTmpl := &ServiceTemplate{
		Name:           "gitea",
		DefaultVersion: "latest",
		Docker: DockerConfig{
			Image: "gitea/gitea:{{ .Version }}",
			Ports: []PortConfig{
				{Container: 3000, Protocol: "tcp"},
			},
			Environment: map[string]string{
				"GITEA__APP_NAME": "{{ .Config.app_name }}",
			},
			HealthCheck: HealthCheckConfig{
				Test:     []string{"CMD", "wget", "--spider", "-q", "http://localhost:3000/api/healthz"},
				Interval: "10s",
				Timeout:  "5s",
				Retries:  5,
			},
		},
		Connection: ConnectionConfig{
			Internal: "http://{{ .ContainerName }}:3000",
			External: "{{ if .Domain }}https://{{ .Domain }}{{ else }}http://{{ .ContainerName }}:3000{{ end }}",
		},
	}
	catalog.templates["gitea"] = webTmpl

	tests := []struct {
		name       string
		userID     string
		tmplName   string
		config     map[string]interface{}
		setupMock  func(*mockInstanceRepository, *mockContainerRepository, *mockDockerClient, chan struct{})
		wantErr    bool
		wantStatus models.ServiceInstanceStatus
		// wantAsync indicates the test expects background goroutine completion
		wantAsync bool
	}{
		{
			name:     "Success",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.UserID != "user1" || s.TemplateName != "redis" || s.Name != "my-redis" {
						return errors.New("unexpected instance data")
					}
					s.ID = "inst-1"
					return nil
				}
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusPending}, nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.ID != "inst-1" {
						return errors.New("unexpected id in update")
					}
					// Signal that the async goroutine completed
					close(done)
					return nil
				}
				mc.createFunc = func(ctx context.Context, c *models.Container) error {
					if c.ServiceInstanceID.String != "inst-1" {
						return errors.New("unexpected instance id in container")
					}
					return nil
				}
			},
			wantErr:    false,
			wantStatus: models.ServiceStatusPending,
			wantAsync:  true,
		},
		{
			name:     "Deploy with domain sets Traefik labels",
			userID:   "user1",
			tmplName: "gitea",
			config:   map[string]interface{}{"name": "my-gitea", "domain": "gitea.example.com", "app_name": "My Gitea"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-2"
					if !s.Domain.Valid || s.Domain.String != "gitea.example.com" {
						return errors.New("expected domain to be set on instance")
					}
					return nil
				}
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusPending}, nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					close(done)
					return nil
				}
				mc.createFunc = func(ctx context.Context, c *models.Container) error {
					return nil
				}
				md.createContainerFunc = func(ctx context.Context, opts docker.ContainerOptions) (string, error) {
					// Verify Traefik labels are set
					if opts.Labels == nil {
						return "", errors.New("expected labels to be set")
					}
					if opts.Labels["traefik.enable"] != "true" {
						return "", errors.New("expected traefik.enable label")
					}
					routerRule := opts.Labels["traefik.http.routers.goforge-svc-inst-2.rule"]
					if routerRule != "Host(`gitea.example.com`)" {
						return "", fmt.Errorf("unexpected router rule: %s", routerRule)
					}
					portLabel := opts.Labels["traefik.http.services.goforge-svc-inst-2.loadbalancer.server.port"]
					if portLabel != "3000" {
						return "", fmt.Errorf("unexpected port label: %s", portLabel)
					}
					// Verify health check is set
					if opts.RestartPolicy != docker.RestartUnlessStopped {
						return "", fmt.Errorf("unexpected restart policy: %s", opts.RestartPolicy)
					}
					if opts.HealthCheck == nil {
						return "", errors.New("expected health check to be set")
					}
					if len(opts.HealthCheck.Test) == 0 {
						return "", errors.New("expected health check test commands")
					}
					return "docker-id-2", nil
				}
			},
			wantErr:    false,
			wantStatus: models.ServiceStatusPending,
			wantAsync:  true,
		},
		{
			name:     "Deploy without domain has no Traefik routing labels",
			userID:   "user1",
			tmplName: "gitea",
			config:   map[string]interface{}{"name": "my-gitea", "app_name": "My Gitea"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-3"
					return nil
				}
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusPending}, nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					close(done)
					return nil
				}
				mc.createFunc = func(ctx context.Context, c *models.Container) error {
					return nil
				}
				md.createContainerFunc = func(ctx context.Context, opts docker.ContainerOptions) (string, error) {
					if opts.Labels["goforge.managed"] != "true" {
						return "", errors.New("expected goforge.managed label")
					}
					for key := range opts.Labels {
						if strings.Contains(key, ".rule") {
							return "", fmt.Errorf("unexpected routing label %s", key)
						}
					}
					if opts.RestartPolicy != docker.RestartUnlessStopped {
						return "", fmt.Errorf("unexpected restart policy: %s", opts.RestartPolicy)
					}
					if opts.HealthCheck == nil {
						return "", errors.New("expected health check to be set even without domain")
					}
					return "docker-id-3", nil
				}
			},
			wantErr:    false,
			wantStatus: models.ServiceStatusPending,
			wantAsync:  true,
		},
		{
			name:     "Template not found",
			userID:   "user1",
			tmplName: "postgres",
			config:   map[string]interface{}{"name": "my-pg"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
			},
			wantErr: true,
		},
		{
			name:     "Missing name in config",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"foo": "bar"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
			},
			wantErr: true,
		},
		{
			name:     "Repository create error",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return errors.New("db error")
				}
			},
			wantErr: true,
		},
		{
			name:     "Docker create error updates status to failed",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-1"
					return nil
				}
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusPending}, nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					// The async goroutine should set status to failed
					if s.Status != models.ServiceStatusFailed {
						close(done)
						return fmt.Errorf("expected failed status, got %s", s.Status)
					}
					close(done)
					return nil
				}
				md.createContainerFunc = func(ctx context.Context, opts docker.ContainerOptions) (string, error) {
					return "", errors.New("docker error")
				}
			},
			wantErr:    false, // DeployInstance returns nil; error is handled async
			wantStatus: models.ServiceStatusPending,
			wantAsync:  true,
		},
		{
			name:     "Repository update error is handled async",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient, done chan struct{}) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-1"
					return nil
				}
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusPending}, nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					close(done)
					return errors.New("update failed")
				}
			},
			wantErr:    false, // DeployInstance returns nil; error is handled async
			wantStatus: models.ServiceStatusPending,
			wantAsync:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			mockContainerRepo := &mockContainerRepository{}
			mockDocker := &mockDockerClient{}
			done := make(chan struct{})
			if tt.setupMock != nil {
				tt.setupMock(mockRepo, mockContainerRepo, mockDocker, done)
			}

			svc := NewInstanceService(mockRepo, mockContainerRepo, mockDocker, catalog, "goforge_net", false, slog.New(slog.NewTextHandler(io.Discard, nil)))

			got, err := svc.DeployInstance(context.Background(), tt.userID, tt.tmplName, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeployInstance() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != nil {
				if got.Status != tt.wantStatus {
					t.Errorf("DeployInstance() status = %v, want %v", got.Status, tt.wantStatus)
				}
			}

			// Wait for the async goroutine to finish for tests that spawn one
			if tt.wantAsync {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for async deployment goroutine")
				}
			}
		})
	}
}

func TestInstanceService_ListInstances(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		userID    string
		setupMock func(*mockInstanceRepository)
		wantLen   int
		wantErr   bool
	}{
		{
			name:   "Success",
			userID: "user1",
			setupMock: func(m *mockInstanceRepository) {
				m.listByUserIDFunc = func(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
					return []*models.ServiceInstance{
						{ID: "1"}, {ID: "2"},
					}, nil
				}
			},
			wantLen: 2,
			wantErr: false,
		},
		{
			name:   "Error",
			userID: "user1",
			setupMock: func(m *mockInstanceRepository) {
				m.listByUserIDFunc = func(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
					return nil, errors.New("db error")
				}
			},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			if tt.setupMock != nil {
				tt.setupMock(mockRepo)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := NewInstanceService(mockRepo, nil, nil, nil, "goforge_net", false, logger)
			got, err := svc.ListInstances(context.Background(), tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListInstances() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.wantLen {
				t.Errorf("ListInstances() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestInstanceService_GetInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		id        string
		setupMock func(*mockInstanceRepository)
		wantID    string
		wantErr   bool
	}{
		{
			name: "Found",
			id:   "inst-1",
			setupMock: func(m *mockInstanceRepository) {
				m.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: "inst-1"}, nil
				}
			},
			wantID:  "inst-1",
			wantErr: false,
		},
		{
			name: "Not Found",
			id:   "inst-2",
			setupMock: func(m *mockInstanceRepository) {
				m.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			if tt.setupMock != nil {
				tt.setupMock(mockRepo)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := NewInstanceService(mockRepo, nil, nil, nil, "goforge_net", false, logger)
			got, err := svc.GetInstance(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.ID != tt.wantID {
				t.Errorf("GetInstance() ID = %v, want %v", got.ID, tt.wantID)
			}
		})
	}
}

func TestInstanceService_DeleteInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		id        string
		setupMock func(*mockInstanceRepository, *mockContainerRepository, *mockDockerClient)
		wantErr   bool
	}{
		{
			name: "Success",
			id:   "inst-1",
			setupMock: func(m *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mc.getByServiceInstanceIDFunc = func(ctx context.Context, instanceID string) (*models.Container, error) {
					return &models.Container{DockerID: "docker-1"}, nil
				}
				md.stopContainerFunc = func(ctx context.Context, id string) error {
					return nil
				}
				m.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name: "Error",
			id:   "inst-1",
			setupMock: func(m *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mc.getByServiceInstanceIDFunc = func(ctx context.Context, instanceID string) (*models.Container, error) {
					return nil, errors.New("not found")
				}
				m.deleteFunc = func(ctx context.Context, id string) error {
					return errors.New("db error")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			mockContainerRepo := &mockContainerRepository{}
			mockDocker := &mockDockerClient{}
			if tt.setupMock != nil {
				tt.setupMock(mockRepo, mockContainerRepo, mockDocker)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := NewInstanceService(mockRepo, mockContainerRepo, mockDocker, nil, "goforge_net", false, logger)
			err := svc.DeleteInstance(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstanceService_UpdateInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		instance  *models.ServiceInstance
		setupMock func(*mockInstanceRepository)
		wantErr   bool
	}{
		{
			name:     "Success",
			instance: &models.ServiceInstance{ID: "inst-1"},
			setupMock: func(m *mockInstanceRepository) {
				m.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:     "Error",
			instance: &models.ServiceInstance{ID: "inst-1"},
			setupMock: func(m *mockInstanceRepository) {
				m.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return errors.New("db error")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			if tt.setupMock != nil {
				tt.setupMock(mockRepo)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := NewInstanceService(mockRepo, nil, nil, nil, "goforge_net", false, logger)
			err := svc.UpdateInstance(context.Background(), tt.instance)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstanceService_Lifecycle(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name      string
		operation string // "stop", "start", "restart"
		setupMock func(*mockInstanceRepository, *mockContainerRepository, *mockDockerClient)
		wantErr   bool
	}{
		{
			name:      "Stop Success",
			operation: "stop",
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusRunning}, nil
				}
				mc.getByServiceInstanceIDFunc = func(ctx context.Context, instanceID string) (*models.Container, error) {
					return &models.Container{DockerID: "docker-1"}, nil
				}
				md.stopContainerFunc = func(ctx context.Context, id string) error {
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.Status != models.ServiceStatusStopped {
						return errors.New("wrong status")
					}
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "Start Success",
			operation: "start",
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusStopped}, nil
				}
				mc.getByServiceInstanceIDFunc = func(ctx context.Context, instanceID string) (*models.Container, error) {
					return &models.Container{DockerID: "docker-1"}, nil
				}
				md.startContainerFunc = func(ctx context.Context, id string) error {
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.Status != models.ServiceStatusRunning {
						return errors.New("wrong status")
					}
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "Restart Success",
			operation: "restart",
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.getByIDFunc = func(ctx context.Context, id string) (*models.ServiceInstance, error) {
					return &models.ServiceInstance{ID: id, Status: models.ServiceStatusRunning}, nil
				}
				mc.getByServiceInstanceIDFunc = func(ctx context.Context, instanceID string) (*models.Container, error) {
					return &models.Container{DockerID: "docker-1"}, nil
				}
				md.restartContainerFunc = func(ctx context.Context, id string) error {
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.Status != models.ServiceStatusRunning {
						return errors.New("wrong status")
					}
					return nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockRepo := &mockInstanceRepository{}
			mockContainerRepo := &mockContainerRepository{}
			mockDocker := &mockDockerClient{}
			if tt.setupMock != nil {
				tt.setupMock(mockRepo, mockContainerRepo, mockDocker)
			}

			svc := NewInstanceService(mockRepo, mockContainerRepo, mockDocker, nil, "goforge_net", false, logger)

			var err error
			switch tt.operation {
			case "stop":
				err = svc.StopInstance(context.Background(), "inst-1")
			case "start":
				err = svc.StartInstance(context.Background(), "inst-1")
			case "restart":
				err = svc.RestartInstance(context.Background(), "inst-1")
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("%sInstance() error = %v, wantErr %v", tt.operation, err, tt.wantErr)
			}
		})
	}
}

func TestInstanceService_PruneSystem(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name      string
		setupMock func(*mockDockerClient)
		wantErr   bool
	}{
		{
			name: "Success",
			setupMock: func(md *mockDockerClient) {
				md.cleanupSystemFunc = func(ctx context.Context) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name: "Error",
			setupMock: func(md *mockDockerClient) {
				md.cleanupSystemFunc = func(ctx context.Context) error {
					return errors.New("docker error")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockDocker := &mockDockerClient{}
			if tt.setupMock != nil {
				tt.setupMock(mockDocker)
			}

			svc := NewInstanceService(nil, nil, mockDocker, nil, "goforge_net", false, logger)

			err := svc.PruneSystem(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("PruneSystem() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultServiceDomain(t *testing.T) {
	t.Parallel()
	if got := DefaultServiceDomain("vaultwarden", false); got != "vaultwarden.localhost" {
		t.Errorf("standard mode = %q, want vaultwarden.localhost", got)
	}
	if got := DefaultServiceDomain("vaultwarden", true); got != "" {
		t.Errorf("Cloudflare proxy mode = %q, want empty (a .localhost host is unreachable through the tunnel)", got)
	}
}

func TestInstanceService_CloudflareProxyMode(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, mode := range []bool{false, true} {
		if got := NewInstanceService(nil, nil, nil, nil, "goforge_net", mode, logger).CloudflareProxyMode(); got != mode {
			t.Errorf("CloudflareProxyMode() = %v, want %v", got, mode)
		}
	}
}
