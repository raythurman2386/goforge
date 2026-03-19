package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// QueueRepository handles deployment queue data access.
type QueueRepository struct {
	db *sql.DB
}

// NewQueueRepository creates a new QueueRepository.
func NewQueueRepository(db *sql.DB) *QueueRepository {
	return &QueueRepository{db: db}
}

// Enqueue adds a deployment to the queue with pending status.
func (r *QueueRepository) Enqueue(ctx context.Context, item *models.QueueItem) error {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	item.CreatedAt = time.Now()
	item.Status = models.QueueStatusPending

	query := `
		INSERT INTO deployment_queue (id, deployment_id, status, created_at)
		VALUES ($1, $2, $3, $4)`

	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.DeploymentID,
		item.Status,
		item.CreatedAt,
	)
	return err
}

// Dequeue atomically claims the oldest pending job and marks it as processing.
// Returns nil if the queue is empty.
func (r *QueueRepository) Dequeue(ctx context.Context) (*models.QueueItem, error) {
	query := `
		UPDATE deployment_queue
		SET status = $1, started_at = $2
		WHERE id = (
			SELECT id FROM deployment_queue
			WHERE status = $3
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, deployment_id, status, created_at, started_at, finished_at, error_message`

	item := &models.QueueItem{}
	err := r.db.QueryRowContext(ctx, query,
		models.QueueStatusProcessing,
		time.Now(),
		models.QueueStatusPending,
	).Scan(
		&item.ID,
		&item.DeploymentID,
		&item.Status,
		&item.CreatedAt,
		&item.StartedAt,
		&item.FinishedAt,
		&item.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// Complete marks a job as completed.
func (r *QueueRepository) Complete(ctx context.Context, id string) error {
	query := `
		UPDATE deployment_queue
		SET status = $1, finished_at = $2
		WHERE id = $3`

	result, err := r.db.ExecContext(ctx, query,
		models.QueueStatusCompleted,
		time.Now(),
		id,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Fail marks a job as failed with an error message.
func (r *QueueRepository) Fail(ctx context.Context, id, errMsg string) error {
	query := `
		UPDATE deployment_queue
		SET status = $1, finished_at = $2, error_message = $3
		WHERE id = $4`

	result, err := r.db.ExecContext(ctx, query,
		models.QueueStatusFailed,
		time.Now(),
		errMsg,
		id,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// RequeueOrphans resets any processing jobs that have been running longer than
// the specified timeout back to pending status. This recovers from worker crashes.
// Returns the number of items requeued.
func (r *QueueRepository) RequeueOrphans(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	query := `
		UPDATE deployment_queue
		SET status = $1, started_at = NULL
		WHERE status = $2 AND started_at < $3`

	result, err := r.db.ExecContext(ctx, query,
		models.QueueStatusPending,
		models.QueueStatusProcessing,
		cutoff,
	)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}

// GetStats returns counts of jobs in each status.
func (r *QueueRepository) GetStats(ctx context.Context) (*models.QueueStats, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM deployment_queue
		GROUP BY status`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &models.QueueStats{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		switch status {
		case "pending":
			stats.Pending = count
		case "processing":
			stats.Processing = count
		case "completed":
			stats.Completed = count
		case "failed":
			stats.Failed = count
		}
	}
	return stats, rows.Err()
}

// GetByID retrieves a queue item by ID.
func (r *QueueRepository) GetByID(ctx context.Context, id string) (*models.QueueItem, error) {
	query := `
		SELECT id, deployment_id, status, created_at, started_at, finished_at, error_message
		FROM deployment_queue WHERE id = $1`

	item := &models.QueueItem{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.DeploymentID,
		&item.Status,
		&item.CreatedAt,
		&item.StartedAt,
		&item.FinishedAt,
		&item.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return item, err
}

// GetByDeploymentID retrieves a queue item by deployment ID.
func (r *QueueRepository) GetByDeploymentID(ctx context.Context, deploymentID string) (*models.QueueItem, error) {
	query := `
		SELECT id, deployment_id, status, created_at, started_at, finished_at, error_message
		FROM deployment_queue WHERE deployment_id = $1`

	item := &models.QueueItem{}
	err := r.db.QueryRowContext(ctx, query, deploymentID).Scan(
		&item.ID,
		&item.DeploymentID,
		&item.Status,
		&item.CreatedAt,
		&item.StartedAt,
		&item.FinishedAt,
		&item.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return item, err
}
