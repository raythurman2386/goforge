package notifytest

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// SMTPMessage is one message the fake SMTP server accepted.
type SMTPMessage struct {
	From     string
	To       []string
	Data     string
	AuthUser string
	// TLS is whether the session was encrypted (STARTTLS or implicit).
	TLS bool
}

// SMTPServer is a minimal fake SMTP server on 127.0.0.1 for tests: EHLO,
// STARTTLS, AUTH PLAIN, MAIL, RCPT, DATA, RSET, NOOP, QUIT. It never
// relays. A failed AUTH answers 535 with the submitted password in the
// reply text, so tests can prove the notifier drops server text.
type SMTPServer struct {
	Host string
	Port int
	// ClientTLS trusts the server's certificate (set as the notifier's
	// SMTPTLS).
	ClientTLS *tls.Config

	mode       string
	user, pass string
	serverTLS  *tls.Config
	ln         net.Listener

	mu   sync.Mutex
	msgs []SMTPMessage
}

// NewSMTPServer starts a fake server closed at test cleanup. mode is
// "starttls" (plain, offers STARTTLS), "implicit" (implicit TLS), or "none".
// With user set it offers AUTH PLAIN and requires it before MAIL.
func NewSMTPServer(t testing.TB, mode, user, pass string) *SMTPServer {
	t.Helper()
	// Borrow httptest's self-signed certificate (valid for 127.0.0.1).
	hs := httptest.NewUnstartedServer(nil)
	hs.StartTLS()
	cert := hs.TLS.Certificates
	pool := x509.NewCertPool()
	pool.AddCert(hs.Certificate())
	hs.Close()

	s := &SMTPServer{
		Host: "127.0.0.1", mode: mode, user: user, pass: pass,
		serverTLS: &tls.Config{Certificates: cert, MinVersion: tls.VersionTLS12},
		ClientTLS: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "implicit" {
		ln = tls.NewListener(ln, s.serverTLS)
	}
	s.ln = ln
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	s.Port = tcp.Port
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Addr is host:port.
func (s *SMTPServer) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Messages returns a copy of the accepted messages.
func (s *SMTPServer) Messages() []SMTPMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SMTPMessage(nil), s.msgs...)
}

func (s *SMTPServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(c)
	}
}

// smtpSession is one client connection.
type smtpSession struct {
	srv       *SMTPServer
	c         net.Conn
	tp        *textproto.Conn
	encrypted bool
	authed    bool
	authUser  string
	cur       *SMTPMessage
}

func (ss *smtpSession) reply(format string, args ...any) {
	_ = ss.tp.PrintfLine(format, args...) //nolint:errcheck // the next read fails
}

func (s *SMTPServer) session(c net.Conn) {
	ss := &smtpSession{srv: s, c: c, tp: textproto.NewConn(c), encrypted: s.mode == "implicit", authed: s.user == ""}
	defer func() { ss.c.Close() }()
	ss.reply("220 fake.goforge.test ESMTP")
	for {
		line, err := ss.tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		if !ss.handle(strings.ToUpper(verb), arg) {
			return
		}
	}
}

// handle runs one command; false ends the session.
func (ss *smtpSession) handle(verb, arg string) bool {
	switch verb {
	case "EHLO":
		ss.ehlo()
	case "HELO", "NOOP":
		ss.reply("250 ok")
	case "RSET":
		ss.cur = nil
		ss.reply("250 ok")
	case "STARTTLS":
		return ss.startTLS()
	case "AUTH":
		ss.auth(arg)
	case "MAIL":
		if !ss.authed {
			ss.reply("530 auth required")
			return true
		}
		ss.cur = &SMTPMessage{From: addrArg(arg), AuthUser: ss.authUser, TLS: ss.encrypted}
		ss.reply("250 ok")
	case "RCPT":
		if ss.cur == nil {
			ss.reply("503 MAIL first")
			return true
		}
		ss.cur.To = append(ss.cur.To, addrArg(arg))
		ss.reply("250 ok")
	case "DATA":
		return ss.data()
	case "QUIT":
		ss.reply("221 bye")
		return false
	default:
		ss.reply("502 unknown")
	}
	return true
}

func (ss *smtpSession) ehlo() {
	lines := []string{"fake.goforge.test"}
	if ss.srv.mode == "starttls" && !ss.encrypted {
		lines = append(lines, "STARTTLS")
	}
	if ss.srv.user != "" {
		lines = append(lines, "AUTH PLAIN")
	}
	lines = append(lines, "8BITMIME")
	for i, l := range lines {
		sep := "-"
		if i == len(lines)-1 {
			sep = " "
		}
		ss.reply("250%s%s", sep, l)
	}
}

func (ss *smtpSession) startTLS() bool {
	if ss.srv.mode != "starttls" || ss.encrypted {
		ss.reply("502 not here")
		return true
	}
	ss.reply("220 go ahead")
	tc := tls.Server(ss.c, ss.srv.serverTLS)
	if err := tc.Handshake(); err != nil {
		return false
	}
	ss.c, ss.tp, ss.encrypted = tc, textproto.NewConn(tc), true
	return true
}

func (ss *smtpSession) auth(arg string) {
	mech, resp, _ := strings.Cut(arg, " ")
	if !strings.EqualFold(mech, "PLAIN") {
		ss.reply("504 unsupported")
		return
	}
	b, err := base64.StdEncoding.DecodeString(resp)
	if err != nil {
		ss.reply("501 bad base64")
		return
	}
	parts := strings.Split(string(b), "\x00")
	if len(parts) != 3 {
		ss.reply("501 bad PLAIN")
		return
	}
	if parts[1] == ss.srv.user && parts[2] == ss.srv.pass {
		ss.authed, ss.authUser = true, parts[1]
		ss.reply("235 ok")
		return
	}
	ss.reply("535 5.7.8 bad credentials (got password %s)", parts[2])
}

func (ss *smtpSession) data() bool {
	if ss.cur == nil || len(ss.cur.To) == 0 {
		ss.reply("503 RCPT first")
		return true
	}
	ss.reply("354 go")
	b, err := ss.tp.ReadDotBytes()
	if err != nil {
		return false
	}
	ss.cur.Data = string(b)
	ss.srv.mu.Lock()
	ss.srv.msgs = append(ss.srv.msgs, *ss.cur)
	ss.srv.mu.Unlock()
	ss.cur = nil
	ss.reply("250 queued")
	return true
}

// addrArg pulls the address out of "FROM:<a@b> ..." or "TO:<a@b>".
func addrArg(arg string) string {
	if i := strings.Index(arg, "<"); i >= 0 {
		if j := strings.Index(arg[i:], ">"); j > 0 {
			return arg[i+1 : i+j]
		}
	}
	return arg
}
