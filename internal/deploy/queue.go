package deploy

import (
	"context"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// DefaultDeploymentTimeout is the maximum time allowed for a deployment (10 minutes).
const DefaultDeploymentTimeout = 10 * time.Minute

// Worker represents a deployment worker.
type Worker struct {
	service *Service
	queue   chan string // deployment IDs
	wg      sync.WaitGroup
	quit    chan struct{}
	timeout time.Duration // deployment timeout
}

// NewWorker creates a new deployment worker.
func NewWorker(s *Service, bufferSize int, timeout time.Duration) *Worker {
	if timeout <= 0 {
		timeout = DefaultDeploymentTimeout
	}
	return &Worker{
		service: s,
		queue:   make(chan string, bufferSize),
		quit:    make(chan struct{}),
		timeout: timeout,
	}
}

// Start starts the deployment worker.
func (w *Worker) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case id := <-w.queue:
				w.process(id)
			case <-w.quit:
				// Drain remaining queued deployments before exiting
				w.drain()
				return
			}
		}
	}()
}

// drain processes any remaining items in the queue before shutdown.
func (w *Worker) drain() {
	for {
		select {
		case id := <-w.queue:
			w.process(id)
		default:
			return
		}
	}
}

// Stop stops the deployment worker gracefully.
func (w *Worker) Stop() {
	close(w.quit)
	w.wg.Wait()
}

// Enqueue adds a deployment to the queue. Returns false if the queue is full.
func (w *Worker) Enqueue(id string) bool {
	select {
	case w.queue <- id:
		return true
	default:
		w.service.logger.Warn("deployment queue is full, rejecting deployment", "deployment_id", id)
		return false
	}
}

// process handles a single deployment with proper context and timeout.
func (w *Worker) process(id string) {
	// Create context with timeout for this deployment
	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()

	err := w.service.RunPipeline(ctx, id)
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			w.service.logger.Error("deployment timed out", "deployment_id", id, "timeout", w.timeout)
			msg := "deployment timed out after " + w.timeout.String()
			// Use a fresh context for the transition since the deployment context is expired.
			transitionCtx, transitionCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer transitionCancel()
			if transitionErr := w.service.Transition(transitionCtx, id, models.DeploymentStatusFailed, &msg); transitionErr != nil {
				w.service.logger.Error("failed to transition deployment to failed", "deployment_id", id, "error", transitionErr)
			}
			return
		}

		w.service.logger.Error("deployment failed", "deployment_id", id, "error", err)
		msg := err.Error()
		if transitionErr := w.service.Transition(ctx, id, models.DeploymentStatusFailed, &msg); transitionErr != nil {
			w.service.logger.Error("failed to transition deployment to failed", "deployment_id", id, "error", transitionErr)
		}
	}
}
