package httputil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Add(kv[i], kv[i+1])
	}
	return h
}

func TestClientIPFromHeaders(t *testing.T) {
	private := DefaultTrustedProxies()
	tenNet := ParseTrustedProxies([]string{"10.0.0.0/8"})
	loopback := ParseTrustedProxies([]string{"127.0.0.1/32"})

	tests := []struct {
		name    string
		remote  string
		header  http.Header
		trusted []*net.IPNet
		cf      bool // ProxyTrust.CloudflareConnectingIP (CLOUDFLARE_PROXY_MODE)
		want    string
	}{
		{"no trusted proxies ignores every header", "1.2.3.4:5678",
			hdr("X-Forwarded-For", "9.9.9.9", "X-Real-IP", "8.8.8.8", "CF-Connecting-IP", "7.7.7.7"), nil, false, "1.2.3.4"},
		{"untrusted peer ignores every header", "203.0.113.99:5678",
			hdr("X-Forwarded-For", "1.1.1.1", "X-Real-IP", "2.2.2.2", "CF-Connecting-IP", "3.3.3.3"), tenNet, false, "203.0.113.99"},
		{"trusted peer XFF single hop", "10.0.0.1:1234",
			hdr("X-Forwarded-For", "203.0.113.50, 10.0.0.1"), tenNet, false, "203.0.113.50"},
		{"trusted peer X-Real-IP fallback", "10.0.0.1:1234",
			hdr("X-Real-IP", "203.0.113.50"), tenNet, false, "203.0.113.50"},
		{"invalid X-Real-IP ignored", "10.0.0.1:1234",
			hdr("X-Real-IP", "not-an-ip"), tenNet, false, "10.0.0.1"},
		{"no headers returns peer", "10.0.0.1:1234", http.Header{}, tenNet, false, "10.0.0.1"},
		{"no port", "1.2.3.4", http.Header{}, nil, false, "1.2.3.4"},

		// XFF-SPOOF: the client writes the leftmost entries. Only the
		// rightmost untrusted hop was appended by a proxy we trust.
		{"XFF spoofed leftmost is ignored", "172.18.0.5:40000",
			hdr("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 172.18.0.3"), private, false, "198.51.100.7"},
		{"XFF rotating spoof keeps the same key", "172.18.0.5:40000",
			hdr("X-Forwarded-For", "1.2.3.4, 5.5.5.5, 198.51.100.7"), private, false, "198.51.100.7"},
		{"XFF walks past trusted hops only", "127.0.0.1:8080",
			hdr("X-Forwarded-For", "5.6.7.8, 10.0.0.1, 127.0.0.1"), loopback, false, "10.0.0.1"},
		{"XFF whitespace", "127.0.0.1:8080",
			hdr("X-Forwarded-For", "  5.6.7.8 , 9.9.9.9 "), loopback, false, "9.9.9.9"},
		{"XFF multiple header lines", "10.0.0.1:1",
			hdr("X-Forwarded-For", "6.6.6.6", "X-Forwarded-For", "198.51.100.7"), tenNet, false, "198.51.100.7"},
		{"XFF all trusted returns leftmost", "10.0.0.1:1",
			hdr("X-Forwarded-For", "10.0.0.9, 10.0.0.2"), tenNet, false, "10.0.0.9"},
		{"XFF garbage left of trusted hop falls back to last real hop", "10.0.0.1:1",
			hdr("X-Forwarded-For", "evil-key, 10.0.0.2"), tenNet, false, "10.0.0.2"},
		{"XFF garbage as only hop falls back to peer", "10.0.0.1:1",
			hdr("X-Forwarded-For", "evil-key"), tenNet, false, "10.0.0.1"},
		{"XFF beats X-Real-IP", "10.0.0.1:1",
			hdr("X-Forwarded-For", "198.51.100.7", "X-Real-IP", "6.6.6.6"), tenNet, false, "198.51.100.7"},

		// Cloudflare, CLOUDFLARE_PROXY_MODE off (default): CF-Connecting-IP is
		// ignored even from a trusted peer, because Traefik forwards it as sent.
		{"CF mode off: CF ignored from trusted peer, XFF used", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "6.6.6.6", "X-Forwarded-For", "198.51.100.7"), private, false, "198.51.100.7"},
		{"CF mode off: CF ignored from trusted peer, X-Real-IP used", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "6.6.6.6", "X-Real-IP", "198.51.100.8"), private, false, "198.51.100.8"},
		{"CF mode off: CF only header, trusted peer returned", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "6.6.6.6"), private, false, "172.18.0.5"},
		{"CF mode off: CF from untrusted peer ignored", "203.0.113.1:1",
			hdr("CF-Connecting-IP", "198.51.100.9"), private, false, "203.0.113.1"},

		// Cloudflare, CLOUDFLARE_PROXY_MODE on: preferred when the peer is trusted.
		{"CF mode on: CF preferred over XFF", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "198.51.100.9", "X-Forwarded-For", "6.6.6.6, 198.51.100.7"), private, true, "198.51.100.9"},
		{"CF mode on: CF IPv6", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "2001:db8::42"), private, true, "2001:db8::42"},
		{"CF mode on: CF invalid falls through to XFF", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "nope", "X-Forwarded-For", "198.51.100.7"), private, true, "198.51.100.7"},
		{"CF mode on: CF from untrusted peer ignored", "203.0.113.1:1",
			hdr("CF-Connecting-IP", "198.51.100.9"), private, true, "203.0.113.1"},
		{"CF mode on: no trusted proxies ignores CF", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "198.51.100.9"), nil, true, "172.18.0.5"},

		{"IPv6 peer and XFF", "[::1]:8080",
			hdr("X-Forwarded-For", "2001:db8::1"), ParseTrustedProxies([]string{"::1/128"}), false, "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trust := ProxyTrust{Proxies: tt.trusted, CloudflareConnectingIP: tt.cf}
			if got := ClientIPFromHeaders(tt.remote, tt.header, trust); got != tt.want {
				t.Errorf("ClientIPFromHeaders(%q, %v, cf=%v) = %q, want %q", tt.remote, tt.header, tt.cf, got, tt.want)
			}
		})
	}
}

func TestClientIP_Request(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if got := ClientIP(r, NewProxyTrust(nil, true)); got != "198.51.100.9" {
		t.Errorf("ClientIP (Cloudflare mode) = %q, want 198.51.100.9", got)
	}
	if got := ClientIP(r, NewProxyTrust(nil, false)); got != "10.0.0.1" {
		t.Errorf("ClientIP (default) = %q, want 10.0.0.1", got)
	}
}

func TestNewProxyTrust(t *testing.T) {
	def := NewProxyTrust(nil, false)
	if def.CloudflareConnectingIP {
		t.Error("CloudflareConnectingIP must default to false")
	}
	if len(def.Proxies) != len(DefaultTrustedProxies()) {
		t.Errorf("empty TRUSTED_PROXIES: got %d nets, want defaults (%d)", len(def.Proxies), len(DefaultTrustedProxies()))
	}
	custom := NewProxyTrust([]string{"172.18.0.0/16"}, true)
	if !custom.CloudflareConnectingIP || len(custom.Proxies) != 1 || custom.Proxies[0].String() != "172.18.0.0/16" {
		t.Errorf("NewProxyTrust(172.18.0.0/16, true) = %+v", custom)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  int
	}{
		{"empty", nil, 0},
		{"single CIDR", []string{"10.0.0.0/8"}, 1},
		{"single IP", []string{"10.0.0.1"}, 1},
		{"mixed", []string{"10.0.0.0/8", "192.168.1.1", "::1"}, 3},
		{"invalid skipped", []string{"not-an-ip", "10.0.0.0/8"}, 1},
		{"IPv6 plain", []string{"::1"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTrustedProxies(tt.input)
			if len(got) != tt.want {
				t.Errorf("ParseTrustedProxies(%v) returned %d nets, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestDefaultTrustedProxies(t *testing.T) {
	nets := DefaultTrustedProxies()
	if len(nets) == 0 {
		t.Fatal("expected default trusted proxies to be non-empty")
	}

	// Verify loopback is trusted
	lo := net.ParseIP("127.0.0.1")
	found := false
	for _, n := range nets {
		if n.Contains(lo) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 127.0.0.1 to be in default trusted proxies")
	}

	// Verify public IP is NOT trusted
	pub := net.ParseIP("8.8.8.8")
	for _, n := range nets {
		if n.Contains(pub) {
			t.Error("expected 8.8.8.8 to NOT be in default trusted proxies")
		}
	}
}
