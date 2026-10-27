package deploy

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
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
	queueRepo     *repositories.QueueRepository
	pollInterval  time.Duration
	orphanTimeout time.Duration
	timeout       time.Duration
	wg            sync.WaitGroup
	quit          chan struct{}
	stopped       chan struct{}
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
			return
		}
	}

	err := w.service.RunPipeline(ctx, item.DeploymentID)
	if err != nil {
		w.handleFailure(item, err)
		return
	}

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

func (w *Worker) handleFailure(item *models.QueueItem, deploymentErr error) {
	itemCtx, itemCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer itemCancel()

	if errors.Is(deploymentErr, ErrCanceled) {
		w.finishCanceled(item)
		return
	}
	if w.service != nil {
		if current, getErr := w.service.GetDeployment(itemCtx, item.DeploymentID); getErr == nil && current.Status == models.DeploymentStatusCanceled {
			w.finishCanceled(item)
			return
		}
	}

	errMsg := deploymentErr.Error()
	if failErr := w.queueRepo.Fail(itemCtx, item.ID, errMsg); failErr != nil {
		w.service.logger.Error("failed to mark queue item as failed", "queue_id", item.ID, "error", failErr)
	}

	// RunPipeline already records the failed status. Skip a second write when it succeeded.
	if current, getErr := w.service.GetDeployment(itemCtx, item.DeploymentID); getErr == nil && current.Status == models.DeploymentStatusFailed {
		return
	}

	transitionErr := w.service.Transition(itemCtx, item.DeploymentID, models.DeploymentStatusFailed, &errMsg)
	if transitionErr != nil {
		w.service.logger.Error("failed to transition deployment to failed", "deployment_id", item.DeploymentID, "error", transitionErr)
	}
}

// Enqueue adds a deployment to the queue for processing.
func (w *Worker) Enqueue(ctx context.Context, deploymentID string) error {
	enqueueCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	q := &models.QueueItem{
		DeploymentID: deploymentID,
	}

	if err := w.queueRepo.Enqueue(enqueueCtx, q); err != nil {
		return err
	}

	if w.service != nil && w.service.logger != nil {
		w.service.logger.Info("deployment enqueued", "deployment_id", deploymentID, "queue_id", q.ID)
	}
	return nil
}
