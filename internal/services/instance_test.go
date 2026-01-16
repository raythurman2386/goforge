package services

import (
	"context"
	"errors"
	"testing"

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

func TestInstanceService_DeployInstance(t *testing.T) {
	t.Parallel()
	// Setup catalog
	catalog := NewCatalog("dummy")
	tmpl := &ServiceTemplate{
		Name:           "redis",
		DefaultVersion: "7.0",
	}
	catalog.templates["redis"] = tmpl

	tests := []struct {
		name       string
		userID     string
		tmplName   string
		config     map[string]interface{}
		setupMock  func(*mockInstanceRepository)
		wantErr    bool
		wantStatus models.ServiceInstanceStatus
	}{
		{
			name:     "Success",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(m *mockInstanceRepository) {
				m.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.UserID != "user1" || s.TemplateName != "redis" || s.Name != "my-redis" {
						return errors.New("unexpected instance data")
					}
					s.ID = "inst-1"
					return nil
				}
				m.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					if s.ID != "inst-1" {
						return errors.New("unexpected id in update")
					}
					return nil
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
			setupMock: func(m *mockInstanceRepository) {
			},
			wantErr: true,
		},
		{
			name:     "Missing name in config",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"foo": "bar"},
			setupMock: func(m *mockInstanceRepository) {
			},
			wantErr: true,
		},
		{
			name:     "Repository create error",
			userID:   "user1",
			tmplName: "redis",
			config:   map[string]interface{}{"name": "my-redis"},
			setupMock: func(m *mockInstanceRepository) {
				m.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
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
			setupMock: func(m *mockInstanceRepository) {
				m.createFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					s.ID = "inst-1"
					return nil
				}
				m.updateFunc = func(ctx context.Context, s *models.ServiceInstance) error {
					return errors.New("update failed")
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

			svc := NewInstanceService(mockRepo, nil, nil, catalog)

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
			svc := NewInstanceService(mockRepo, nil, nil, nil)
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
			svc := NewInstanceService(mockRepo, nil, nil, nil)
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
		setupMock func(*mockInstanceRepository)
		wantErr   bool
	}{
		{
			name: "Success",
			id:   "inst-1",
			setupMock: func(m *mockInstanceRepository) {
				m.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name: "Error",
			id:   "inst-1",
			setupMock: func(m *mockInstanceRepository) {
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
			if tt.setupMock != nil {
				tt.setupMock(mockRepo)
			}
			svc := NewInstanceService(mockRepo, nil, nil, nil)
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
			svc := NewInstanceService(mockRepo, nil, nil, nil)
			err := svc.UpdateInstance(context.Background(), tt.instance)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateInstance() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
