package deploy

import (
	"context"
	"time"

	"github.com/raythurman2386/goforge/internal/notify"
)

// WithNotifier makes the worker send a deploy.failed alert when a queued
// deployment fails (not when it is canceled).
func (w *Worker) WithNotifier(n notify.Notifier) *Worker {
	w.notifier = n
	return w
}

// notifyFailed sends deploy.failed for the item. The link is the
// deployment page; the name is "<project> / <environment>" when they can
// be read.
func (w *Worker) notifyFailed(ctx context.Context, deploymentID, auditID, errMsg string) {
	if w.notifier == nil {
		return
	}
	ev := notify.Event{
		Kind:     notify.EventDeployFailed,
		Resource: notify.Resource{Type: "deployment", ID: deploymentID},
		Error:    errMsg,
		AuditID:  auditID,
		Time:     time.Now(),
	}
	if w.service != nil {
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if d, err := w.service.GetDeployment(lookupCtx, deploymentID); err == nil && w.service.environmentRepo != nil {
			if env, envErr := w.service.environmentRepo.GetByID(lookupCtx, d.EnvironmentID); envErr == nil {
				ev.Path = "/projects/" + env.ProjectID + "/deployments/" + deploymentID
				ev.Resource.Name = env.Name
				if w.service.projectRepo != nil {
					if p, pErr := w.service.projectRepo.GetByID(lookupCtx, env.ProjectID); pErr == nil {
						ev.Resource.Name = p.Name + " / " + env.Name
					}
				}
			}
		}
	}
	w.notifier.Notify(ctx, ev)
}
