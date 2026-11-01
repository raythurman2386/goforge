package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// defaultSMTPPort is the usual port for a TLS mode.
func defaultSMTPPort(mode string) int {
	switch mode {
	case models.SMTPTLSImplicit:
		return 465
	case models.SMTPTLSNone:
		return 25
	default:
		return 587
	}
}

// isLoopbackHost is the set of hosts net/smtp's PlainAuth accepts without
// TLS. GoForge allows TLS mode none with a username only for these.
func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// smtpAddr is host:port for messages. It never holds the username.
func smtpAddr(c models.SMTPSettings) string {
	port := c.Port
	if port == 0 {
		port = defaultSMTPPort(c.TLS)
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// buildEmail renders one plain-text message (quoted-printable, UTF-8).
func buildEmail(from *mail.Address, to []*mail.Address, p Payload) []byte {
	var body strings.Builder
	body.WriteString(p.Title + "\r\n\r\n")
	if p.Error != "" {
		body.WriteString(p.Error + "\r\n\r\n")
	}
	body.WriteString("Resource: " + p.Resource.Type + " " + p.Resource.ID + "\r\n")
	if p.Link != "" {
		body.WriteString("Link: " + p.Link + "\r\n")
	}
	if p.AuditID != "" {
		body.WriteString("Audit ID: " + p.AuditID + "\r\n")
	}
	body.WriteString("Time: " + p.Time + "\r\n\r\n")
	body.WriteString("-- \r\nSent by GoForge. Change this in Settings > Profile > Notifications.\r\n")

	tos := make([]string, len(to))
	for i, a := range to {
		tos[i] = a.String()
	}
	var id [12]byte
	_, _ = rand.Read(id[:]) //nolint:errcheck // crypto/rand.Read does not fail
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", strings.Join(tos, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", strings.Join(strings.Fields(p.Title), " ")))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(id[:])+"@goforge>")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	h("X-GoForge-Event", headerSafe(p.Event))
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(body.String())) //nolint:errcheck // writes to a bytes.Buffer
	_ = qp.Close()
	return b.Bytes()
}

// smtpStepError turns a failed SMTP step into a SendError. The server's
// reply text is dropped (it can echo what was sent); only the code is kept.
func smtpStepError(addr, step string, err error) error {
	var tpErr *textproto.Error
	if errors.As(err, &tpErr) {
		return &SendError{Message: fmt.Sprintf("SMTP %s answered %d at %s", addr, tpErr.Code, step)}
	}
	return &SendError{Message: fmt.Sprintf("SMTP %s failed at %s", addr, step)}
}

// errUntrusted reports a TLS certificate the client does not trust.
func errUntrusted(err error, addr string) error {
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return &SendError{Message: "TLS certificate of SMTP " + addr + " is not trusted"}
	}
	return nil
}

// smtpTLSConfig is base (or a fresh config) for host, TLS 1.2 or newer.
func smtpTLSConfig(base *tls.Config, host string) *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if base != nil {
		cfg = base.Clone()
		if cfg.MinVersion < tls.VersionTLS12 {
			cfg.MinVersion = tls.VersionTLS12
		}
	}
	cfg.ServerName = host
	return cfg
}

// dialSMTP connects, with TLS first for implicit mode.
func dialSMTP(ctx context.Context, tlsCfg *tls.Config, c models.SMTPSettings, addr string) (net.Conn, error) {
	d := &net.Dialer{}
	var conn net.Conn
	var err error
	if c.TLS == models.SMTPTLSImplicit {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err == nil {
		return conn, nil
	}
	if ctx.Err() != nil {
		return nil, &SendError{Message: "timed out"}
	}
	if uErr := errUntrusted(err, addr); uErr != nil {
		return nil, uErr
	}
	return nil, &SendError{Message: "could not reach SMTP " + addr}
}

// smtpSecure runs STARTTLS (when the mode asks for it) and AUTH PLAIN
// (when a username is set).
func smtpSecure(cl *smtp.Client, tlsCfg *tls.Config, c models.SMTPSettings, addr string) error {
	if c.TLS == models.SMTPTLSStartTLS {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return &SendError{Message: "SMTP " + addr + " does not offer STARTTLS"}
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			if uErr := errUntrusted(err, addr); uErr != nil {
				return uErr
			}
			return smtpStepError(addr, "STARTTLS", err)
		}
	}
	if c.Username == "" {
		return nil
	}
	if ok, _ := cl.Extension("AUTH"); !ok {
		return &SendError{Message: "SMTP " + addr + " does not offer AUTH"}
	}
	if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
		// Never the server text or the credentials.
		return &SendError{Message: "SMTP " + addr + " rejected the username or password"}
	}
	return nil
}

// smtpSend runs MAIL, RCPT, and DATA.
func smtpSend(cl *smtp.Client, from *mail.Address, to []*mail.Address, p Payload, addr string) error {
	if err := cl.Mail(from.Address); err != nil {
		return smtpStepError(addr, "MAIL FROM", err)
	}
	for _, a := range to {
		if err := cl.Rcpt(a.Address); err != nil {
			return smtpStepError(addr, "RCPT TO", err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return smtpStepError(addr, "DATA", err)
	}
	if _, err = w.Write(buildEmail(from, to, p)); err != nil {
		return smtpStepError(addr, "DATA", err)
	}
	if err = w.Close(); err != nil {
		return smtpStepError(addr, "DATA", err)
	}
	return nil
}

// deliverEmail sends one payload over SMTP. Errors are *SendError and
// never contain the username, password, or the server's reply text.
func deliverEmail(ctx context.Context, base *tls.Config, c models.SMTPSettings, p Payload) error {
	addr := smtpAddr(c)
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return &SendError{Message: "the From address is not valid"}
	}
	to, err := mail.ParseAddressList(c.To)
	if err != nil || len(to) == 0 {
		return &SendError{Message: "the To list is not valid"}
	}
	tlsCfg := smtpTLSConfig(base, c.Host)
	conn, err := dialSMTP(ctx, tlsCfg, c, addr)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl) //nolint:errcheck // the AfterFunc close still bounds the send
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return smtpStepError(addr, "greeting", err)
	}
	defer cl.Close()
	if err = cl.Hello("goforge"); err != nil {
		return smtpStepError(addr, "EHLO", err)
	}
	if err := smtpSecure(cl, tlsCfg, c, addr); err != nil {
		return err
	}
	if err := smtpSend(cl, from, to, p, addr); err != nil {
		return err
	}
	_ = cl.Quit() //nolint:errcheck // the message was accepted at DATA
	return nil
}
