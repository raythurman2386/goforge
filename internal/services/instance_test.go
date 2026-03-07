package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// mockInstanceRepository implements InstanceRepository for testing.
type mockInstanceRepository struct {
	createFunc       func(ctx context.Context, s *models.ServiceInstance) error
	getByIDFunc      func(ctx context.Context, id string) (*models.ServiceInstance, error)
	listByUserIDFunc func(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
	updateFunc       func(ctx context.Context, s *models.ServiceInstance) error
	deleteFunc       func(ctx context.Context, id string) error
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
	restartContainerFunc func(ctx context.Context, id string) error
	cleanupSystemFunc    func(ctx context.Context) error
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
		setupMock  func(*mockInstanceRepository, *mockContainerRepository, *mockDockerClient)
		wantErr    bool
		wantStatus models.ServiceInstanceStatus
		checkOpts  func(t *testing.T, opts docker.ContainerOptions)
	}{
		{
			name:     "Success",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.UserID != "user1" || s.TemplateName != "redis" || s.Name != "my-redis" {
						return errors.New("unexpected instance data")
					}
					s.ID = "inst-1"
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.ID != "inst-1" {
						return errors.New("unexpected id in update")
					}
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
			wantStatus: models.ServiceStatusRunning,
		},
		{
			name:     "Deploy with domain sets Traefik labels",
			userID:   "user1",
			tmplName: "gitea",
			config:   map[string]interface{}{"name": "my-gitea", "domain": "gitea.example.com", "app_name": "My Gitea"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-2"
					if !s.Domain.Valid || s.Domain.String != "gitea.example.com" {
						return errors.New("expected domain to be set on instance")
					}
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
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
			wantStatus: models.ServiceStatusRunning,
		},
		{
			name:     "Deploy without domain has no Traefik routing labels",
			userID:   "user1",
			tmplName: "gitea",
			config:   map[string]interface{}{"name": "my-gitea", "app_name": "My Gitea"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-3"
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return nil
				}
				mc.createFunc = func(ctx context.Context, c *models.Container) error {
					return nil
				}
				md.createContainerFunc = func(ctx context.Context, opts docker.ContainerOptions) (string, error) {
					// Verify no Traefik routing labels when no domain
					if opts.Labels != nil {
						return "", errors.New("expected no labels when domain is empty")
					}
					// But health check should still be set
					if opts.HealthCheck == nil {
						return "", errors.New("expected health check to be set even without domain")
					}
					return "docker-id-3", nil
				}
			},
			wantErr:    false,
			wantStatus: models.ServiceStatusRunning,
		},
		{
			name:     "Template not found",
			userID:   "user1",
			tmplName: "postgres",
			config:   map[string]interface{}{"name": "my-pg"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
			},
			wantErr: true,
		},
		{
			name:     "Missing name in config",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"foo": "bar"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
			},
			wantErr: true,
		},
		{
			name:     "Repository create error",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return errors.New("db error")
				}
			},
			wantErr: true,
		},
		{
			name:     "Repository update error",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-1"
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return errors.New("update failed")
				}
			},
			wantErr: true,
		},
		{
			name:     "Docker create error",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(mi *mockInstanceRepository, mc *mockContainerRepository, md *mockDockerClient) {
				mi.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-1"
					return nil
				}
				mi.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return nil
				}
				md.createContainerFunc = func(ctx context.Context, opts docker.ContainerOptions) (string, error) {
					return "", errors.New("docker error")
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
