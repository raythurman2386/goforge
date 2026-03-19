package repositories

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// Queue tests are not parallel because they share the global queue table

func cleanupQueue(t *testing.T, db *sql.DB) {
	t.Helper()
	_, _ = db.Exec("DELETE FROM deployment_queue")
}

func TestQueueRepository_Enqueue(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	q := &models.QueueItem{
		DeploymentID: deployment.ID,
	}

	err := repo.Enqueue(ctx, q)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if q.ID == "" {
		t.Error("Enqueue() did not set ID")
	}
	if q.CreatedAt.IsZero() {
		t.Error("Enqueue() did not set CreatedAt")
	}
	if q.Status != models.QueueStatusPending {
		t.Errorf("Enqueue() status = %q, want %q", q.Status, models.QueueStatusPending)
	}
}

func TestQueueRepository_Dequeue(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	dequeued, err := repo.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	if dequeued == nil {
		t.Fatal("Dequeue() returned nil, want item")
	}
	if dequeued.ID != item.ID {
		t.Errorf("Dequeue() ID = %q, want %q", dequeued.ID, item.ID)
	}
	if dequeued.Status != models.QueueStatusProcessing {
		t.Errorf("Dequeue() status = %q, want %q", dequeued.Status, models.QueueStatusProcessing)
	}
	if !dequeued.StartedAt.Valid {
		t.Error("Dequeue() did not set StartedAt")
	}
}

func TestQueueRepository_Dequeue_EmptyQueue(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	dequeued, err := repo.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	if dequeued != nil {
		t.Errorf("Dequeue() returned %v, want nil for empty queue", dequeued.ID)
	}
}

func TestQueueRepository_Dequeue_IgnoresProcessing(t *testing.T) {
	// Not parallel - tests share global queue state
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if _, err := repo.Dequeue(ctx); err != nil {
		t.Fatalf("First Dequeue() error = %v", err)
	}

	item2 := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item2); err != nil {
		t.Fatalf("Enqueue() second item error = %v", err)
	}

	dequeued, err := repo.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Second Dequeue() error = %v", err)
	}

	if dequeued.ID != item2.ID {
		t.Errorf("Dequeue() returned %q, want newly enqueued item %q", dequeued.ID, item2.ID)
	}
}

func TestQueueRepository_Complete(t *testing.T) {
	db := getTestDB(t)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	dequeued, _ := repo.Dequeue(ctx)

	err := repo.Complete(ctx, dequeued.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	updated, err := repo.GetByID(ctx, dequeued.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if updated.Status != models.QueueStatusCompleted {
		t.Errorf("Complete() status = %q, want %q", updated.Status, models.QueueStatusCompleted)
	}
	if !updated.FinishedAt.Valid {
		t.Error("Complete() did not set FinishedAt")
	}
}

func TestQueueRepository_Fail(t *testing.T) {
	db := getTestDB(t)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	dequeued, _ := repo.Dequeue(ctx)

	errMsg := "deployment failed: build error"
	err := repo.Fail(ctx, dequeued.ID, errMsg)
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	updated, err := repo.GetByID(ctx, dequeued.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if updated.Status != models.QueueStatusFailed {
		t.Errorf("Fail() status = %q, want %q", updated.Status, models.QueueStatusFailed)
	}
	if !updated.ErrorMessage.Valid {
		t.Error("Fail() did not set ErrorMessage")
	}
	if updated.ErrorMessage.String != errMsg {
		t.Errorf("Fail() error_message = %q, want %q", updated.ErrorMessage.String, errMsg)
	}
	if !updated.FinishedAt.Valid {
		t.Error("Fail() did not set FinishedAt")
	}
}

func TestQueueRepository_RequeueOrphans(t *testing.T) {
	db := getTestDB(t)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	dequeued, _ := repo.Dequeue(ctx)

	orphanedTime := time.Now().Add(-15 * time.Minute)
	_, err := db.ExecContext(ctx,
		"UPDATE deployment_queue SET started_at = $1 WHERE id = $2",
		orphanedTime, dequeued.ID)
	if err != nil {
		t.Fatalf("Manual update to simulate orphan error = %v", err)
	}

	requeued, err := repo.RequeueOrphans(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RequeueOrphans() error = %v", err)
	}

	if requeued != 1 {
		t.Errorf("RequeueOrphans() = %d, want 1", requeued)
	}

	updated, err := repo.GetByID(ctx, dequeued.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if updated.Status != models.QueueStatusPending {
		t.Errorf("RequeueOrphans() status = %q, want %q", updated.Status, models.QueueStatusPending)
	}
}

func TestQueueRepository_GetStats(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.Pending != 0 || stats.Processing != 0 || stats.Completed != 0 || stats.Failed != 0 {
		t.Errorf("GetStats() initial = %+v, want all zeros", stats)
	}

	deployment1 := createTestDeployment(t, db, envID)
	item1 := &models.QueueItem{DeploymentID: deployment1.ID}
	repo.Enqueue(ctx, item1)

	deployment2 := createTestDeployment(t, db, envID)
	item2 := &models.QueueItem{DeploymentID: deployment2.ID}
	repo.Enqueue(ctx, item2)

	repo.Dequeue(ctx)

	stats, err = repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.Pending != 1 {
		t.Errorf("GetStats().Pending = %d, want 1", stats.Pending)
	}
	if stats.Processing != 1 {
		t.Errorf("GetStats().Processing = %d, want 1", stats.Processing)
	}
}

func TestQueueRepository_GetByID(t *testing.T) {
	db := getTestDB(t)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	repo.Enqueue(ctx, item)

	fetched, err := repo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if fetched.ID != item.ID {
		t.Errorf("GetByID() ID = %q, want %q", fetched.ID, item.ID)
	}
}

func TestQueueRepository_GetByID_NotFound(t *testing.T) {
	db := getTestDB(t)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestQueueRepository_GetByDeploymentID(t *testing.T) {
	db := getTestDB(t)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	repo.Enqueue(ctx, item)

	fetched, err := repo.GetByDeploymentID(ctx, deployment.ID)
	if err != nil {
		t.Fatalf("GetByDeploymentID() error = %v", err)
	}

	if fetched.DeploymentID != deployment.ID {
		t.Errorf("GetByDeploymentID() deployment_id = %q, want %q", fetched.DeploymentID, deployment.ID)
	}
}

func TestQueueRepository_GetByDeploymentID_NotFound(t *testing.T) {
	db := getTestDB(t)

	repo := NewQueueRepository(db)
	ctx := context.Background()

	_, err := repo.GetByDeploymentID(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByDeploymentID() error = %v, want ErrNotFound", err)
	}
}

func createTestEnvironment(t *testing.T, db *sql.DB) string {
	t.Helper()

	ctx := context.Background()

	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("queue-test")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projectRepo := NewProjectRepository(db)
	project := &models.Project{
		UserID: user.ID,
		Name:   "test-project-" + uniqueID(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{
		ProjectID: project.ID,
		Name:      "test-" + uniqueID(),
		Branch:    sql.NullString{String: "main", Valid: true},
	}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	return env.ID
}

func createTestDeployment(t *testing.T, db *sql.DB, envID string) *models.Deployment {
	t.Helper()

	ctx := context.Background()

	deploymentRepo := NewDeploymentRepository(db)
	deployment := &models.Deployment{
		EnvironmentID: envID,
		Status:        models.DeploymentStatusPending,
		TriggerType:   models.TriggerTypeManual,
	}
	if err := deploymentRepo.Create(ctx, deployment); err != nil {
		t.Fatalf("failed to create deployment: %v", err)
	}

	return deployment
}
