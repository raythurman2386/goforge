package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

var (
	testDBOnce sync.Once
	errTestDB  error
	testDB     *sql.DB

	testCounter uint64
	testRunID   = fmt.Sprintf("%d", time.Now().UnixNano())
)

func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDBOnce.Do(func() {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
		}

		cfg := &config.Config{
			DatabaseType: config.DatabaseTypePostgres,
			DatabaseURL:  dsn,
		}

		goforgeDB, err := database.New(cfg)
		if err != nil {
			errTestDB = fmt.Errorf("failed to connect to postgres: %w", err)
			return
		}

		if err := database.MigrateUp(goforgeDB, cfg); err != nil {
			goforgeDB.Close()
			errTestDB = fmt.Errorf("failed to run migrations: %w", err)
			return
		}

		testDB = goforgeDB.DB
	})

	if errTestDB != nil {
		t.Skipf("skipping test; %v", errTestDB)
	}

	return testDB
}

func uniqueID() string {
	return fmt.Sprintf("%s-%d", testRunID, atomic.AddUint64(&testCounter, 1))
}

func createTestEnvironment(t *testing.T, db *sql.DB) string {
	t.Helper()

	ctx := context.Background()

	userRepo := repositories.NewUserRepository(db, nil)
	user := &models.User{Email: "worker-test-" + uniqueID() + "@test.local"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projectRepo := repositories.NewProjectRepository(db)
	project := &models.Project{
		UserID: user.ID,
		Name:   "test-project-" + uniqueID(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := repositories.NewEnvironmentRepository(db)
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

	deploymentRepo := repositories.NewDeploymentRepository(db)
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

func cleanupQueue(t *testing.T, db *sql.DB) {
	t.Helper()
	_, _ = db.Exec("DELETE FROM deployment_queue")
}

func TestWorker_Enqueue(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	queueRepo := repositories.NewQueueRepository(db)
	worker := &Worker{
		queueRepo: queueRepo,
	}

	err := worker.Enqueue(context.Background(), deployment.ID)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	item, err := queueRepo.GetByDeploymentID(context.Background(), deployment.ID)
	if err != nil {
		t.Fatalf("GetByDeploymentID() error = %v", err)
	}
	if item.DeploymentID != deployment.ID {
		t.Errorf("Enqueue() deployment_id = %q, want %q", item.DeploymentID, deployment.ID)
	}
	if item.Status != models.QueueStatusPending {
		t.Errorf("Enqueue() status = %q, want %q", item.Status, models.QueueStatusPending)
	}
}

func TestWorker_StartStop(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	queueRepo := repositories.NewQueueRepository(db)

	worker := &Worker{
		queueRepo:     queueRepo,
		pollInterval:  100 * time.Millisecond,
		orphanTimeout: DefaultOrphanTimeout,
		timeout:       DefaultDeploymentTimeout,
		quit:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}

	worker.Start()
	time.Sleep(50 * time.Millisecond)
	worker.Stop()
}

func TestWorker_RequeueOrphans(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)

	envID := createTestEnvironment(t, db)
	deployment := createTestDeployment(t, db, envID)

	queueRepo := repositories.NewQueueRepository(db)
	ctx := context.Background()

	item := &models.QueueItem{DeploymentID: deployment.ID}
	if err := queueRepo.Enqueue(ctx, item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	_, _ = queueRepo.Dequeue(ctx)

	orphanedTime := time.Now().Add(-15 * time.Minute)
	_, err := db.ExecContext(ctx,
		"UPDATE deployment_queue SET started_at = $1 WHERE id = $2",
		orphanedTime, item.ID)
	if err != nil {
		t.Fatalf("failed to update started_at: %v", err)
	}

	worker := &Worker{
		queueRepo:     queueRepo,
		orphanTimeout: 5 * time.Minute,
	}

	worker.requeueOrphans()

	updated, err := queueRepo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if updated.Status != models.QueueStatusPending {
		t.Errorf("requeueOrphans() status = %q, want %q", updated.Status, models.QueueStatusPending)
	}
}
