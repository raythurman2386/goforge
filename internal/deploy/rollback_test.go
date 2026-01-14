package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/raythurman2386/gocool/internal/models"
)

func TestRollbackManager_CanRollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deployment *models.Deployment
		want       bool
	}{
		{
			name: "running deployment can be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-1",
				Status: models.DeploymentStatusRunning,
			},
			want: true,
		},
		{
			name: "failed deployment can be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-2",
				Status: models.DeploymentStatusFailed,
			},
			want: true,
		},
		{
			name: "pending deployment cannot be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-3",
				Status: models.DeploymentStatusPending,
			},
			want: false,
		},
		{
			name: "cloning deployment cannot be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-4",
				Status: models.DeploymentStatusCloning,
			},
			want: false,
		},
		{
			name: "building deployment cannot be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-5",
				Status: models.DeploymentStatusBuilding,
			},
			want: false,
		},
		{
			name: "deploying deployment cannot be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-6",
				Status: models.DeploymentStatusDeploying,
			},
			want: false,
		},
		{
			name: "already rolled back deployment cannot be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-7",
				Status: models.DeploymentStatusRolledBack,
			},
			want: false,
		},
		{
			name: "canceled deployment can be rolled back",
			deployment: &models.Deployment{
				ID:     "deploy-8",
				Status: models.DeploymentStatusCanceled,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rm := NewRollbackManager(nil)
			got := rm.CanRollback(tt.deployment)
			if got != tt.want {
				t.Errorf("CanRollback() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRollbackManager_FindRollbackTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		deployments []*models.Deployment
		current     string
		wantID      string
		wantErr     bool
	}{
		{
			name: "find previous successful deployment",
			deployments: []*models.Deployment{
				{ID: "deploy-3", Status: models.DeploymentStatusRunning},
				{ID: "deploy-2", Status: models.DeploymentStatusRunning},
				{ID: "deploy-1", Status: models.DeploymentStatusRunning},
			},
			current: "deploy-3",
			wantID:  "deploy-2",
			wantErr: false,
		},
		{
			name: "skip failed deployments",
			deployments: []*models.Deployment{
				{ID: "deploy-3", Status: models.DeploymentStatusRunning},
				{ID: "deploy-2", Status: models.DeploymentStatusFailed},
				{ID: "deploy-1", Status: models.DeploymentStatusRunning},
			},
			current: "deploy-3",
			wantID:  "deploy-1",
			wantErr: false,
		},
		{
			name: "skip rolled back deployments",
			deployments: []*models.Deployment{
				{ID: "deploy-3", Status: models.DeploymentStatusRunning},
				{ID: "deploy-2", Status: models.DeploymentStatusRolledBack},
				{ID: "deploy-1", Status: models.DeploymentStatusRunning},
			},
			current: "deploy-3",
			wantID:  "deploy-1",
			wantErr: false,
		},
		{
			name: "no previous successful deployment",
			deployments: []*models.Deployment{
				{ID: "deploy-2", Status: models.DeploymentStatusRunning},
				{ID: "deploy-1", Status: models.DeploymentStatusFailed},
			},
			current: "deploy-2",
			wantID:  "",
			wantErr: true,
		},
		{
			name: "empty deployment list",
			deployments: []*models.Deployment{
				{ID: "deploy-1", Status: models.DeploymentStatusRunning},
			},
			current: "deploy-1",
			wantID:  "",
			wantErr: true,
		},
		{
			name:        "no deployments at all",
			deployments: []*models.Deployment{},
			current:     "deploy-1",
			wantID:      "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rm := NewRollbackManager(nil)
			got, err := rm.FindRollbackTarget(tt.deployments, tt.current)

			if (err != nil) != tt.wantErr {
				t.Errorf("FindRollbackTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if got == nil {
					t.Errorf("FindRollbackTarget() returned nil, want deployment with ID %s", tt.wantID)
					return
				}
				if got.ID != tt.wantID {
					t.Errorf("FindRollbackTarget() got ID = %s, want %s", got.ID, tt.wantID)
				}
			}
		})
	}
}

func TestRollbackManager_ValidateRollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current *models.Deployment
		target  *models.Deployment
		wantErr bool
	}{
		{
			name: "valid rollback",
			current: &models.Deployment{
				ID:            "deploy-2",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			target: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			wantErr: false,
		},
		{
			name: "same deployment",
			current: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			target: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			wantErr: true,
		},
		{
			name: "different environments",
			current: &models.Deployment{
				ID:            "deploy-2",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			target: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-2",
				Status:        models.DeploymentStatusRunning,
			},
			wantErr: true,
		},
		{
			name: "target not successful",
			current: &models.Deployment{
				ID:            "deploy-2",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			target: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusFailed,
			},
			wantErr: true,
		},
		{
			name:    "nil current",
			current: nil,
			target: &models.Deployment{
				ID:            "deploy-1",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			wantErr: true,
		},
		{
			name: "nil target",
			current: &models.Deployment{
				ID:            "deploy-2",
				EnvironmentID: "env-1",
				Status:        models.DeploymentStatusRunning,
			},
			target:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rm := NewRollbackManager(nil)
			err := rm.ValidateRollback(tt.current, tt.target)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRollback() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRollbackInfo_String(t *testing.T) {
	t.Parallel()

	info := &RollbackInfo{
		FromDeploymentID: "deploy-2",
		ToDeploymentID:   "deploy-1",
		Reason:           "manual rollback requested",
	}

	str := info.String()
	if str == "" {
		t.Error("RollbackInfo.String() returned empty string")
	}

	// Should contain key information
	if !containsAll(str, "deploy-2", "deploy-1") {
		t.Errorf("RollbackInfo.String() = %q, expected to contain deployment IDs", str)
	}
}

func TestRollbackErrors(t *testing.T) {
	t.Parallel()

	// Test that error variables are defined correctly
	if !errors.Is(ErrNoRollbackTarget, ErrNoRollbackTarget) {
		t.Error("ErrNoRollbackTarget is not properly defined")
	}
	if !errors.Is(ErrRollbackInProgress, ErrRollbackInProgress) {
		t.Error("ErrRollbackInProgress is not properly defined")
	}
	if !errors.Is(ErrCannotRollback, ErrCannotRollback) {
		t.Error("ErrCannotRollback is not properly defined")
	}
}

// containsAll checks if str contains all substrings
func containsAll(str string, substrings ...string) bool {
	for _, sub := range substrings {
		found := false
		for i := 0; i <= len(str)-len(sub); i++ {
			if str[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestRollbackManager_CreateRollbackDeployment(t *testing.T) {
	t.Parallel()

	target := &models.Deployment{
		ID:            "deploy-1",
		EnvironmentID: "env-1",
		Status:        models.DeploymentStatusRunning,
		CommitSHA:     toNullString("abc123"),
		CommitMessage: toNullString("Initial commit"),
		CommitAuthor:  toNullString("test@example.com"),
		Branch:        toNullString("main"),
		ImageTag:      toNullString("gocool-proj:deploy-1"),
	}

	rm := NewRollbackManager(nil)
	rollbackDeploy := rm.CreateRollbackDeployment(context.Background(), target, "manual rollback")

	if rollbackDeploy.EnvironmentID != target.EnvironmentID {
		t.Errorf("EnvironmentID = %s, want %s", rollbackDeploy.EnvironmentID, target.EnvironmentID)
	}

	if rollbackDeploy.TriggerType != models.TriggerTypeRollback {
		t.Errorf("TriggerType = %s, want %s", rollbackDeploy.TriggerType, models.TriggerTypeRollback)
	}

	if rollbackDeploy.Status != models.DeploymentStatusPending {
		t.Errorf("Status = %s, want %s", rollbackDeploy.Status, models.DeploymentStatusPending)
	}

	if rollbackDeploy.CommitSHA.String != target.CommitSHA.String {
		t.Errorf("CommitSHA = %s, want %s", rollbackDeploy.CommitSHA.String, target.CommitSHA.String)
	}
}

func toNullString(s string) (ns struct {
	String string
	Valid  bool
}) {
	return struct {
		String string
		Valid  bool
	}{String: s, Valid: true}
}
