package audit

import (
	"context"
	"time"
)

// DaysFunc returns the retention in force. 0 means keep forever.
type DaysFunc func(context.Context) (int, error)

// RunSealer seals until ctx is canceled. It runs once immediately, then
// every interval (SealInterval when interval is 0).
func (c *Chain) RunSealer(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = SealInterval
	}
	c.sealTick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sealTick(ctx)
		}
	}
}

func (c *Chain) sealTick(ctx context.Context) {
	n, err := c.SealOnce(ctx)
	if err != nil && ctx.Err() == nil {
		c.logger().Error("audit sealer failed", "error", err)
		return
	}
	if n > 0 {
		c.logger().Info("audit sealer sealed rows", "count", n)
		// Refresh the activity-page status. The daily check is what logs
		// the anchor line; this does not.
		c.noteVerify(ctx, false)
	}
}

// RunPurge waits initial (PurgeStartupDelay when 0), purges, then purges
// every interval (DailyInterval when 0) until ctx is canceled.
func (c *Chain) RunPurge(ctx context.Context, days DaysFunc, initial, interval time.Duration) {
	if initial <= 0 {
		initial = PurgeStartupDelay
	}
	if interval <= 0 {
		interval = DailyInterval
	}
	timer := time.NewTimer(initial)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
	}
	c.purgeTick(ctx, days)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.purgeTick(ctx, days)
		}
	}
}

func (c *Chain) purgeTick(ctx context.Context, days DaysFunc) {
	if days == nil {
		return
	}
	n, err := days(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger().Error("audit retention days unreadable", "error", err)
		}
		return
	}
	deleted, err := c.PurgeOnce(ctx, n)
	switch {
	case err != nil && ctx.Err() == nil:
		c.logger().Error("audit retention purge failed", "error", err)
	default:
		c.logger().Info("audit retention purge finished", "deleted", deleted, "retention_days", n)
	}
}

// RunVerify waits initial (VerifyStartupDelay when 0), seals anything still
// waiting, verifies, then repeats every interval (DailyInterval when 0).
// Success logs the chain head. Failure logs at error. No notifier is sent.
func (c *Chain) RunVerify(ctx context.Context, initial, interval time.Duration) {
	if initial <= 0 {
		initial = VerifyStartupDelay
	}
	if interval <= 0 {
		interval = DailyInterval
	}
	timer := time.NewTimer(initial)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
	}
	c.verifyTick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.verifyTick(ctx)
		}
	}
}

func (c *Chain) verifyTick(ctx context.Context) {
	if err := c.catchUp(ctx); err != nil && ctx.Err() == nil {
		c.logger().Error("audit sealer failed", "error", err)
	}
	c.noteVerify(ctx, true)
}

// noteVerify stores the latest chain check for the activity page.
// logSuccess is set for the daily anchor line and not for a seal refresh.
func (c *Chain) noteVerify(ctx context.Context, logSuccess bool) {
	seq, hash, fail, err := c.Verify(ctx, 0)
	switch {
	case err != nil && ctx.Err() == nil:
		c.logger().Error("audit chain verification failed", "error", err)
	case fail != nil:
		c.status.set(IntegrityView{Checked: true, OK: false, Seq: fail.Seq, At: time.Now()})
		c.logger().Error(FormatFailed(fail.Seq, fail.Reason))
	default:
		c.status.set(IntegrityView{Checked: true, OK: true, Seq: seq, At: time.Now()})
		if logSuccess {
			c.logger().Info(FormatVerified(seq, hash))
		}
	}
}

// catchUp seals until a pass finds nothing older than the seal window.
func (c *Chain) catchUp(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := c.SealOnce(ctx)
		if err != nil || n == 0 {
			return err
		}
	}
}
