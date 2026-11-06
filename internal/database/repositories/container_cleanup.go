package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// ContainerCleanupRepository persists app-container removal work. A missing
// Docker object can therefore still have its requested DB-row cleanup retried.
type ContainerCleanupRepository struct{ db *sql.DB }

// NewContainerCleanupRepository creates a cleanup-intent repository.
func NewContainerCleanupRepository(db *sql.DB) *ContainerCleanupRepository {
	return &ContainerCleanupRepository{db: db}
}

// Add records the intent idempotently, retaining its original creation time.
func (r *ContainerCleanupRepository) Add(ctx context.Context, c *models.ContainerCleanup) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO container_cleanup
		(docker_id, project_id, environment_id, reason) VALUES ($1, $2, $3, $4)
		ON CONFLICT (docker_id) DO NOTHING`, c.DockerID, c.ProjectID, c.EnvironmentID, c.Reason)
	return err
}

// List returns every outstanding host and DB-row cleanup intent.
func (r *ContainerCleanupRepository) List(ctx context.Context) ([]*models.ContainerCleanup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT docker_id, project_id, environment_id, reason, created_at
		FROM container_cleanup ORDER BY created_at, docker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ContainerCleanup
	for rows.Next() {
		c := &models.ContainerCleanup{}
		if err := rows.Scan(&c.DockerID, &c.ProjectID, &c.EnvironmentID, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListForParent returns cleanup work for a project or environment, making a
// retry independent of whether Docker still lists the removed host object.
func (r *ContainerCleanupRepository) ListForParent(ctx context.Context, column, id string) ([]*models.ContainerCleanup, error) {
	var query string
	switch column {
	case "goforge.project.id":
		query = `SELECT docker_id, project_id, environment_id, reason, created_at FROM container_cleanup
			WHERE project_id = $1 ORDER BY created_at, docker_id`
	case "goforge.environment.id":
		query = `SELECT docker_id, project_id, environment_id, reason, created_at FROM container_cleanup
			WHERE environment_id = $1 ORDER BY created_at, docker_id`
	default:
		return nil, fmt.Errorf("unsupported cleanup parent column %q", column)
	}
	rows, err := r.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ContainerCleanup
	for rows.Next() {
		c := &models.ContainerCleanup{}
		if err := rows.Scan(&c.DockerID, &c.ProjectID, &c.EnvironmentID, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Owner returns the stored tenant attribution for an automatic orphan cleanup.
func (r *ContainerCleanupRepository) Owner(ctx context.Context, dockerID string) (sql.NullString, error) {
	var userID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT p.user_id FROM container_cleanup cc
		JOIN projects p ON p.id = cc.project_id WHERE cc.docker_id = $1`, dockerID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, ErrNotFound
	}
	return userID, err
}

// Complete removes the intent only after its corresponding DB row is gone.
func (r *ContainerCleanupRepository) Complete(ctx context.Context, dockerID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM container_cleanup WHERE docker_id = $1`, dockerID)
	return err
}

// Retain records cleanup requested by a deploy/rollback recording failure or
// retirement. Live-parent pending intents reconcile immediately rather than
// retaining their successful container row after host removal.
func (r *ContainerCleanupRepository) Retain(ctx context.Context, c *models.ContainerCleanup) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	return r.Add(ctx, c)
}
