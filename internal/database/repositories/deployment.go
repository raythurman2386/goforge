package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/gocool/internal/models"
)

// DeploymentRepository handles deployment data access.
type DeploymentRepository struct {
	db *sql.DB
}

// NewDeploymentRepository creates a new DeploymentRepository.
func NewDeploymentRepository(db *sql.DB) *DeploymentRepository {
	return &DeploymentRepository{db: db}
}

// Create creates a new deployment.
func (r *DeploymentRepository) Create(ctx context.Context, deployment *models.Deployment) error {
	if deployment.ID == "" {
		deployment.ID = uuid.New().String()
	}
	deployment.CreatedAt = time.Now()

	query := `
		INSERT INTO deployments (id, environment_id, status, trigger_type, commit_sha,
			commit_message, commit_author, branch, image_tag, error_message,
			started_at, finished_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	_, err := r.db.ExecContext(ctx, query,
		deployment.ID,
		deployment.EnvironmentID,
		deployment.Status,
		deployment.TriggerType,
		deployment.CommitSHA,
		deployment.CommitMessage,
		deployment.CommitAuthor,
		deployment.Branch,
		deployment.ImageTag,
		deployment.ErrorMessage,
		deployment.StartedAt,
		deployment.FinishedAt,
		deployment.CreatedAt,
	)
	return err
}

// GetByID retrieves a deployment by ID.
func (r *DeploymentRepository) GetByID(ctx context.Context, id string) (*models.Deployment, error) {
	query := `
		SELECT id, environment_id, status, trigger_type, commit_sha, commit_message,
			commit_author, branch, image_tag, error_message, started_at, finished_at, created_at
		FROM deployments WHERE id = $1`

	d := &models.Deployment{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&d.ID,
		&d.EnvironmentID,
		&d.Status,
		&d.TriggerType,
		&d.CommitSHA,
		&d.CommitMessage,
		&d.CommitAuthor,
		&d.Branch,
		&d.ImageTag,
		&d.ErrorMessage,
		&d.StartedAt,
		&d.FinishedAt,
		&d.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

// ListByEnvironmentID retrieves deployments for an environment.
func (r *DeploymentRepository) ListByEnvironmentID(ctx context.Context, envID string, limit int) ([]*models.Deployment, error) {
	query := `
		SELECT id, environment_id, status, trigger_type, commit_sha, commit_message,
			commit_author, branch, image_tag, error_message, started_at, finished_at, created_at
		FROM deployments WHERE environment_id = $1 ORDER BY created_at DESC LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, envID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []*models.Deployment
	for rows.Next() {
		d := &models.Deployment{}
		if err := rows.Scan(
			&d.ID,
			&d.EnvironmentID,
			&d.Status,
			&d.TriggerType,
			&d.CommitSHA,
			&d.CommitMessage,
			&d.CommitAuthor,
			&d.Branch,
			&d.ImageTag,
			&d.ErrorMessage,
			&d.StartedAt,
			&d.FinishedAt,
			&d.CreatedAt,
		); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, rows.Err()
}

// GetLatestByEnvironmentID retrieves the latest deployment for an environment.
func (r *DeploymentRepository) GetLatestByEnvironmentID(ctx context.Context, envID string) (*models.Deployment, error) {
	deployments, err := r.ListByEnvironmentID(ctx, envID, 1)
	if err != nil {
		return nil, err
	}
	if len(deployments) == 0 {
		return nil, ErrNotFound
	}
	return deployments[0], nil
}

// UpdateStatus updates the status of a deployment.
func (r *DeploymentRepository) UpdateStatus(ctx context.Context, id string, status models.DeploymentStatus, errorMsg *string) error {
	var query string
	var args []interface{}

	switch {
	case status == models.DeploymentStatusFailed || status == models.DeploymentStatusRunning ||
		status == models.DeploymentStatusRolledBack || status == models.DeploymentStatusCanceled:
		// Terminal state - set finished_at
		query = `UPDATE deployments SET status = $1, error_message = $2, finished_at = $3 WHERE id = $4`
		args = []interface{}{status, errorMsg, time.Now(), id}
	case status == models.DeploymentStatusCloning:
		// First state - set started_at
		query = `UPDATE deployments SET status = $1, started_at = $2 WHERE id = $3`
		args = []interface{}{status, time.Now(), id}
	default:
		query = `UPDATE deployments SET status = $1 WHERE id = $2`
		args = []interface{}{status, id}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
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

// DeploymentLogRepository handles deployment log data access.
type DeploymentLogRepository struct {
	db *sql.DB
}

// NewDeploymentLogRepository creates a new DeploymentLogRepository.
func NewDeploymentLogRepository(db *sql.DB) *DeploymentLogRepository {
	return &DeploymentLogRepository{db: db}
}

// Create creates a new deployment log entry.
func (r *DeploymentLogRepository) Create(ctx context.Context, log *models.DeploymentLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}

	query := `
		INSERT INTO deployment_logs (id, deployment_id, timestamp, level, message, source)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.db.ExecContext(ctx, query,
		log.ID,
		log.DeploymentID,
		log.Timestamp,
		log.Level,
		log.Message,
		log.Source,
	)
	return err
}

// ListByDeploymentID retrieves logs for a deployment.
func (r *DeploymentLogRepository) ListByDeploymentID(ctx context.Context, deploymentID string) ([]*models.DeploymentLog, error) {
	query := `
		SELECT id, deployment_id, timestamp, level, message, source
		FROM deployment_logs WHERE deployment_id = $1 ORDER BY timestamp`

	rows, err := r.db.QueryContext(ctx, query, deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.DeploymentLog
	for rows.Next() {
		log := &models.DeploymentLog{}
		if err := rows.Scan(
			&log.ID,
			&log.DeploymentID,
			&log.Timestamp,
			&log.Level,
			&log.Message,
			&log.Source,
		); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
