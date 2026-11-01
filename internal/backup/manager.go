package backup

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/notify"
)

// Manager owns the nightly scheduler inside serve and swaps it when an admin
// saves new backup settings, so a change applies without a restart.
//
// Apply cancels the old scheduler loop and starts a new one. A run that
// already started keeps its config and the advisory lock and finishes; the
// next run uses the new settings. Shutdown is the parent context passed to
// NewManager; Wait returns once every loop and run has stopped.
type Manager struct {
	parent context.Context
	build  func(cfg config.BackupConfig) (*Setup, error)
	log    *slog.Logger
	red    func(extra ...config.BackupConfig) *Redactor
	notify notify.Notifier

	mu     sync.Mutex
	cfg    config.BackupConfig
	source string
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager builds a Manager for serve. Nothing runs until Apply.
func NewManager(parent context.Context, app *config.Config, db *sql.DB, dockerClient *docker.Client, log *slog.Logger, version string) *Manager {
	return &Manager{
		parent: parent,
		build: func(cfg config.BackupConfig) (*Setup, error) {
			return NewFor(app, cfg, db, dockerClient, log, version)
		},
		log: log,
		red: func(extra ...config.BackupConfig) *Redactor { return RedactorFor(app, extra...) },
	}
}

// WithNotifier makes every scheduled run started after this call send a
// backup.failed alert when it fails. Call it before the first Apply.
func (m *Manager) WithNotifier(n notify.Notifier) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notify = n
	return m
}

// Current returns the config in effect and where it came from.
func (m *Manager) Current() (cfg config.BackupConfig, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg, m.source
}

// Apply makes cfg the config in effect and restarts the scheduler for it.
func (m *Manager) Apply(cfg config.BackupConfig, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.cfg, m.source = cfg, source
	log := m.log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("source", source)
	if m.parent.Err() != nil {
		return
	}
	setup, err := m.build(cfg)
	switch {
	case errors.Is(err, ErrDisabled):
		log.Info("backups disabled")
		return
	case err != nil:
		msg := err.Error()
		if m.red != nil {
			msg = m.red(cfg).Redact(msg)
		}
		log.Error("backups not started", "error", msg)
		return
	case !setup.Scheduled:
		log.Info("backups enabled, nightly schedule off; use goforge backup run")
		return
	}
	setup.Runner.Notifier = m.notify
	loopCtx, cancel := context.WithCancel(m.parent)
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		setup.Runner.ScheduleLoop(loopCtx, m.parent, setup.Schedule)
	}()
}

// Wait blocks until every scheduler loop and in-flight run has returned.
// Cancel the parent context first.
func (m *Manager) Wait() {
	m.wg.Wait()
}
