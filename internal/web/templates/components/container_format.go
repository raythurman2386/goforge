// Package components holds the shared templ UI pieces.
package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
)

// HumanBytes formats a byte count with binary units (KiB, MiB, ...).
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// uptime formats how long ago start was, to the minute.
func uptime(start, now time.Time) string {
	d := now.Sub(start)
	if d < time.Minute {
		return "less than a minute"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	mins := int(d/time.Minute) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// containerStarted is the Started row: uptime for a running container, the
// start time otherwise, or a dash when Docker reported none.
func containerStarted(s *docker.InspectSummary, now time.Time) string {
	t, ok := s.Started()
	if !ok {
		return "—"
	}
	stamp := t.UTC().Format("2006-01-02 15:04 UTC")
	if s.Running {
		return "Up " + uptime(t, now) + " (since " + stamp + ")"
	}
	return stamp
}

// containerHealth is the health row; containers without a healthcheck say so.
func containerHealth(s *docker.InspectSummary) string {
	if s.Health == "" {
		return "No healthcheck"
	}
	return s.Health
}

// containerLimits describes the memory and CPU limits.
func containerLimits(s *docker.InspectSummary) string {
	mem := "no memory limit"
	if s.Limits.MemoryBytes > 0 {
		mem = HumanBytes(s.Limits.MemoryBytes) + " memory"
	}
	cpu := "no CPU limit"
	if s.Limits.CPUs > 0 {
		cpu = strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", s.Limits.CPUs), "0"), ".") + " CPUs"
	}
	return mem + ", " + cpu
}

// containerPorts lists published bindings as host:port -> container port.
// Exposed ports without a host binding are listed by container port only.
func containerPorts(s *docker.InspectSummary) []string {
	out := make([]string, 0, len(s.Ports))
	for _, p := range s.Ports {
		if p.HostPort == "" {
			out = append(out, p.Container+" (not published)")
			continue
		}
		host := p.HostIP
		if host == "" {
			host = "0.0.0.0"
		}
		out = append(out, host+":"+p.HostPort+" → "+p.Container)
	}
	return out
}
