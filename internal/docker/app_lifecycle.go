package docker

import (
	"errors"
	"sync"
)

// ErrAppDeleting stops a deploy, rollback, or retire from creating or changing
// app containers while their project is being deleted.
var ErrAppDeleting = errors.New("project is being deleted")

// AppLifecycle serializes app-container creation and retirement with project
// deletion. A successful deletion leaves the project closed to later creates.
// The control plane shares one instance between project and deploy services.
type AppLifecycle struct {
	mu       sync.Mutex
	projects map[string]*appLifecycleEntry
}

type appLifecycleEntry struct {
	mu       sync.Mutex
	deleting bool
}

// NewAppLifecycle creates an empty lifecycle coordinator.
func NewAppLifecycle() *AppLifecycle {
	return &AppLifecycle{projects: make(map[string]*appLifecycleEntry)}
}

func (l *AppLifecycle) entry(projectID string) *appLifecycleEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.projects == nil {
		l.projects = make(map[string]*appLifecycleEntry)
	}
	e := l.projects[projectID]
	if e == nil {
		e = &appLifecycleEntry{}
		l.projects[projectID] = e
	}
	return e
}

// Create acquires a project's app-container creation/retirement lock.
func (l *AppLifecycle) Create(projectID string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	e := l.entry(projectID)
	e.mu.Lock()
	if e.deleting {
		e.mu.Unlock()
		return nil, ErrAppDeleting
	}
	return e.mu.Unlock, nil
}

// Delete runs fn with exclusive ownership of the project's app lifecycle.
// Creation and retirement wait until fn returns. They remain rejected after a
// successful project deletion; a failed deletion permits repair and retry.
func (l *AppLifecycle) Delete(projectID string, fn func() error) error {
	return l.run(projectID, true, fn)
}

// Exclusive blocks creates only for the duration of fn. It is used for partial
// environment deletion and orphan cleanup where sibling environments stay live.
func (l *AppLifecycle) Exclusive(projectID string, fn func() error) error {
	return l.run(projectID, false, fn)
}

func (l *AppLifecycle) run(projectID string, terminal bool, fn func() error) error {
	if l == nil {
		return fn()
	}
	e := l.entry(projectID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.deleting {
		return ErrAppDeleting
	}
	e.deleting = true
	err := fn()
	if err != nil || !terminal {
		e.deleting = false
	}
	return err
}
