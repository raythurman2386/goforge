package notify

import (
	"regexp"
	"strings"
)

const (
	redacted       = "[redacted]"
	maxErrorLen    = 500
	minSecretToRed = 4
)

var (
	// scheme://user:password@host keeps the user and drops the password.
	urlUserinfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]*):[^@\s]*@`)
	// password=..., token: ..., api_key=..., secret_key: ..., and similar
	// pairs (any key containing one of the words).
	kvSecretRe = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)[a-z0-9_.-]*)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&"']+)`)
	// Authorization: Bearer <token> / Basic <creds>.
	authHeaderRe = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`)
)

// Redactor scrubs secrets from text before it leaves GoForge: known secret
// values, URL passwords, key=value pairs whose key names a secret, and
// bearer or basic credentials. It also collapses the text to one line and
// caps it at 500 bytes.
type Redactor struct {
	secrets []string
	extra   func(string) string
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

// WithFunc adds another redaction pass (for example the backup Redactor,
// which knows the app and S3 secrets) that runs first.
func (r *Redactor) WithFunc(f func(string) string) *Redactor {
	r.extra = f
	return r
}

// Redact returns s with secrets removed.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		if r.extra != nil {
			s = r.extra(s)
		}
		for _, sec := range r.secrets {
			s = strings.ReplaceAll(s, sec, redacted)
		}
	}
	s = urlUserinfoRe.ReplaceAllString(s, "${1}:"+redacted+"@")
	s = kvSecretRe.ReplaceAllString(s, "${1}${2}"+redacted)
	s = authHeaderRe.ReplaceAllString(s, "${1} "+redacted)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrorLen {
		s = strings.ToValidUTF8(s[:maxErrorLen], "") + "…"
	}
	return s
}
