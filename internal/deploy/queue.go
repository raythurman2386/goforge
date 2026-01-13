package deploy

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/gocool/internal/models"
)

// Worker represents a deployment worker.
type Worker struct {
	service *Service
	queue   chan string // deployment IDs
	wg      sync.WaitGroup
	quit    chan struct{}
}

// NewWorker creates a new deployment worker.
func NewWorker(s *Service, bufferSize int) *Worker {
	return &Worker{
		service: s,
		queue:   make(chan string, bufferSize),
		quit:    make(chan struct{}),
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
				return
			}
		}
	}()
}

// Stop stops the deployment worker.
func (w *Worker) Stop() {
	close(w.quit)
	w.wg.Wait()
}

// Enqueue adds a deployment to the queue.
func (w *Worker) Enqueue(id string) {
	w.queue <- id
}

func (w *Worker) process(id string) {
	ctx := context.Background() // Use background context for long-running deployments

	err := w.service.RunPipeline(ctx, id)
	if err != nil {
		fmt.Printf("deployment %s failed: %v\n", id, err)
		msg := err.Error()
		if transitionErr := w.service.Transition(ctx, id, models.DeploymentStatusFailed, &msg); transitionErr != nil {
			fmt.Printf("failed to transition deployment %s to failed: %v\n", id, transitionErr)
		}
	}
}
