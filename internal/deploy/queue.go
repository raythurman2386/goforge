package deploy

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

// Default values for deployment queue settings.
const (
	DefaultDeploymentTimeout = 10 * time.Minute
	DefaultPollInterval      = 1 * time.Second
	DefaultOrphanTimeout     = 10 * time.Minute
)

// Worker processes deployment jobs from a persistent queue.
type Worker struct {
	service       *Service
	queueRepo     queueStore
	pollInterval  time.Duration
	orphanTimeout time.Duration
	timeout       time.Duration
	wg            sync.WaitGroup
	quit          chan struct{}
	stopped       chan struct{}
	audit         services.AuditRunStore
}

// queueStore is the deployment_queue table as the worker uses it;
// *repositories.QueueRepository implements it.
type queueStore interface {
	Enqueue(ctx context.Context, item *models.QueueItem) error
	Dequeue(ctx context.Context) (*models.QueueItem, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id, errMsg string) error
	RequeueOrphans(ctx context.Context, olderThan time.Duration) (int, error)
	DeletePendingByDeploymentID(ctx context.Context, deploymentID string) error
}

// WithAudit makes the worker write deployment.run.completed, .failed, or
// .canceled when a deployment queued with an audit id (models.WithAuditID on
// the Enqueue context) ends. The row's details.audit_id is that id.
func (w *Worker) WithAudit(store services.AuditRunStore) *Worker {
	w.audit = store
	return w
}

// NewWorker creates a new deployment worker.
func NewWorker(s *Service, qr *repositories.QueueRepository, pollInterval, orphanTimeout, timeout time.Duration) *Worker {
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	if orphanTimeout <= 0 {
		orphanTimeout = DefaultOrphanTimeout
	}
	if timeout <= 0 {
		timeout = DefaultDeploymentTimeout
	}
	return &Worker{
		service:       s,
		queueRepo:     qr,
		pollInterval:  pollInterval,
		orphanTimeout: orphanTimeout,
		timeout:       timeout,
		quit:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
}

// Start begins processing deployments from the queue.
func (w *Worker) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		close(w.stopped)
		w.run()
	}()
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	close(w.quit)
	w.wg.Wait()
}

func (w *Worker) run() {
	if w.service != nil && w.service.logger != nil {
		w.service.logger.Info("deployment worker started")
	}

	w.requeueOrphans()

	for {
		select {
		case <-w.quit:
			if w.service != nil && w.service.logger != nil {
				w.service.logger.Info("deployment worker shutting down")
			}
			return
		default:
			w.processNext()
		}
	}
}

func (w *Worker) requeueOrphans() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	count, err := w.queueRepo.RequeueOrphans(ctx, w.orphanTimeout)
	if err != nil {
		if w.service != nil && w.service.logger != nil {
			w.service.logger.Error("failed to requeue orphans", "error", err)
		}
		return
	}
	if count > 0 {
		if w.service != nil && w.service.logger != nil {
			w.service.logger.Info("requeued orphaned deployments", "count", count)
		}
	}
}

func (w *Worker) processNext() {
	ctx, cancel := context.WithTimeout(context.Background(), w.pollInterval)
	defer cancel()

	item, err := w.queueRepo.Dequeue(ctx)
	if err != nil {
		if w.service != nil && w.service.logger != nil {
			w.service.logger.Error("failed to dequeue deployment", "error", err)
		}
		time.Sleep(w.pollInterval)
		return
	}

	if item == nil {
		time.Sleep(w.pollInterval)
		return
	}

	w.processItem(item)
}

func (w *Worker) processItem(item *models.QueueItem) {
	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()
	if w.service != nil {
		w.service.trackRun(item.DeploymentID, cancel)
		defer w.service.untrackRun(item.DeploymentID)
	}

	if w.service != nil && w.service.logger != nil {
		w.service.logger.Info("processing deployment", "deployment_id", item.DeploymentID, "queue_id", item.ID)
	}

	if w.service != nil {
		current, getErr := w.service.GetDeployment(ctx, item.DeploymentID)
		if getErr == nil && current.Status == models.DeploymentStatusCanceled {
			w.finishCanceled(item)
			w.recordOutcome(item, models.AuditRunCanceled, "")
			return
		}
	}

	err := w.service.RunPipeline(ctx, item.DeploymentID)
	if err != nil {
		if w.handleFailure(item, err) {
			w.recordOutcome(item, models.AuditRunCanceled, "")
		} else {
			w.recordOutcome(item, models.AuditRunFailed, err.Error())
		}
		return
	}
	w.recordOutcome(item, models.AuditRunCompleted, "")

	if completeErr := w.queueRepo.Complete(ctx, item.ID); completeErr != nil {
		if w.service != nil && w.service.logger != nil {
			w.service.logger.Error("failed to mark queue item as completed", "queue_id", item.ID, "error", completeErr)
		}
	}
}

func (w *Worker) finishCanceled(item *models.QueueItem) {
	itemCtx, itemCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer itemCancel()
	if err := w.queueRepo.Complete(itemCtx, item.ID); err != nil && w.service != nil && w.service.logger != nil {
		w.service.logger.Error("failed to complete canceled queue item", "queue_id", item.ID, "error", err)
	}
}

// DropPending removes a deployment that is still waiting in the queue.
func (w *Worker) DropPending(ctx context.Context, deploymentID string) error {
	if w == nil || w.queueRepo == nil {
		return nil
	}
	return w.queueRepo.DeletePendingByDeploymentID(ctx, deploymentID)
}

// recordOutcome writes deployment.run.<result> linked to the queue item's
// audit id. Items queued without one (webhooks, older rows) write nothing.
func (w *Worker) recordOutcome(item *models.QueueItem, result, errMsg string) {
	if w.audit == nil || !item.AuditID.Valid || item.AuditID.String == "" {
		return
	}
	status := string(models.DeploymentStatusRunning)
	switch result {
	case models.AuditRunFailed:
		status = string(models.DeploymentStatusFailed)
	case models.AuditRunCanceled:
		status = string(models.DeploymentStatusCanceled)
	}
	var logger *slog.Logger
	if w.service != nil {
		logger = w.service.logger
	}
	services.RecordRunOutcome(w.audit, logger, item.AuditID.String, services.RunOutcome{
		Run:          models.AuditRunDeployment,
		ResourceType: models.ResourceTypeDeployment,
		ResourceID:   item.DeploymentID,
		Result:       result,
		Error:        errMsg,
		Status:       status,
	})
}

// handleFailure records a failed pipeline run. It returns true when the
// deployment was canceled rather than failed.
func (w *Worker) handleFailure(item *models.QueueItem, deploymentErr error) (canceled bool) {
	itemCtx, itemCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer itemCancel()

	if errors.Is(deploymentErr, ErrCanceled) {
		w.finishCanceled(item)
		return true
	}
	if w.service != nil {
		if current, getErr := w.service.GetDeployment(itemCtx, item.DeploymentID); getErr == nil && current.Status == models.DeploymentStatusCanceled {
			w.finishCanceled(item)
			return true
		}
	}

	errMsg := deploymentErr.Error()
	if failErr := w.queueRepo.Fail(itemCtx, item.ID, errMsg); failErr != nil {
		w.service.logger.Error("failed to mark queue item as failed", "queue_id", item.ID, "error", failErr)
	}

	// RunPipeline already records the failed status. Skip a second write when it succeeded.
	if current, getErr := w.service.GetDeployment(itemCtx, item.DeploymentID); getErr == nil && current.Status == models.DeploymentStatusFailed {
		return false
	}

	transitionErr := w.service.Transition(itemCtx, item.DeploymentID, models.DeploymentStatusFailed, &errMsg)
	if transitionErr != nil {
		w.service.logger.Error("failed to transition deployment to failed", "deployment_id", item.DeploymentID, "error", transitionErr)
	}
	return false
}

// Enqueue adds a deployment to the queue for processing. An audit id on ctx
// (models.WithAuditID) is stored with the item so the final outcome links
// back to the row that queued it.
func (w *Worker) Enqueue(ctx context.Context, deploymentID string) error {
	enqueueCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	q := &models.QueueItem{
		DeploymentID: deploymentID,
	}
	if id := models.AuditIDFrom(ctx); id != "" {
		q.AuditID = sql.NullString{String: id, Valid: true}
	}

	if err := w.queueRepo.Enqueue(enqueueCtx, q); err != nil {
		return err
	}

	if w.service != nil && w.service.logger != nil {
		w.service.logger.Info("deployment enqueued", "deployment_id", deploymentID, "queue_id", q.ID)
	}
	return nil
}
