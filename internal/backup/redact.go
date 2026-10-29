package backup

import (
	"regexp"
	"strings"
)

const (
	redacted       = "[redacted]"
	maxSummaryLen  = 500
	minSecretToRed = 4
)

var urlUserinfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]*):[^@\s]*@`)

// Redactor removes known secret values from text that may be logged or
// stored in backup_runs.error_summary.
type Redactor struct {
	secrets []string
}

// NewRedactor builds a Redactor for the given secret values. Empty and very
// short values are ignored so they do not shred normal text.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		if len(s) >= minSecretToRed {
			r.secrets = append(r.secrets, s)
		}
	}
	return r
}

// Redact replaces secrets and URL passwords, and collapses the text to one
// short line.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		for _, sec := range r.secrets {
			s = strings.ReplaceAll(s, sec, redacted)
		}
	}
	s = urlUserinfoRe.ReplaceAllString(s, "${1}:"+redacted+"@")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxSummaryLen {
		s = s[:maxSummaryLen] + "…"
	}
	return s
}
