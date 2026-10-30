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
		want    string
	}{
		{"no trusted proxies ignores every header", "1.2.3.4:5678",
			hdr("X-Forwarded-For", "9.9.9.9", "X-Real-IP", "8.8.8.8", "CF-Connecting-IP", "7.7.7.7"), nil, "1.2.3.4"},
		{"untrusted peer ignores every header", "203.0.113.99:5678",
			hdr("X-Forwarded-For", "1.1.1.1", "X-Real-IP", "2.2.2.2", "CF-Connecting-IP", "3.3.3.3"), tenNet, "203.0.113.99"},
		{"trusted peer XFF single hop", "10.0.0.1:1234",
			hdr("X-Forwarded-For", "203.0.113.50, 10.0.0.1"), tenNet, "203.0.113.50"},
		{"trusted peer X-Real-IP fallback", "10.0.0.1:1234",
			hdr("X-Real-IP", "203.0.113.50"), tenNet, "203.0.113.50"},
		{"invalid X-Real-IP ignored", "10.0.0.1:1234",
			hdr("X-Real-IP", "not-an-ip"), tenNet, "10.0.0.1"},
		{"no headers returns peer", "10.0.0.1:1234", http.Header{}, tenNet, "10.0.0.1"},
		{"no port", "1.2.3.4", http.Header{}, nil, "1.2.3.4"},

		// XFF-SPOOF: the client writes the leftmost entries. Only the
		// rightmost untrusted hop was appended by a proxy we trust.
		{"XFF spoofed leftmost is ignored", "172.18.0.5:40000",
			hdr("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 172.18.0.3"), private, "198.51.100.7"},
		{"XFF rotating spoof keeps the same key", "172.18.0.5:40000",
			hdr("X-Forwarded-For", "1.2.3.4, 5.5.5.5, 198.51.100.7"), private, "198.51.100.7"},
		{"XFF walks past trusted hops only", "127.0.0.1:8080",
			hdr("X-Forwarded-For", "5.6.7.8, 10.0.0.1, 127.0.0.1"), loopback, "10.0.0.1"},
		{"XFF whitespace", "127.0.0.1:8080",
			hdr("X-Forwarded-For", "  5.6.7.8 , 9.9.9.9 "), loopback, "9.9.9.9"},
		{"XFF multiple header lines", "10.0.0.1:1",
			hdr("X-Forwarded-For", "6.6.6.6", "X-Forwarded-For", "198.51.100.7"), tenNet, "198.51.100.7"},
		{"XFF all trusted returns leftmost", "10.0.0.1:1",
			hdr("X-Forwarded-For", "10.0.0.9, 10.0.0.2"), tenNet, "10.0.0.9"},
		{"XFF garbage left of trusted hop falls back to last real hop", "10.0.0.1:1",
			hdr("X-Forwarded-For", "evil-key, 10.0.0.2"), tenNet, "10.0.0.2"},
		{"XFF garbage as only hop falls back to peer", "10.0.0.1:1",
			hdr("X-Forwarded-For", "evil-key"), tenNet, "10.0.0.1"},
		{"XFF beats X-Real-IP", "10.0.0.1:1",
			hdr("X-Forwarded-For", "198.51.100.7", "X-Real-IP", "6.6.6.6"), tenNet, "198.51.100.7"},

		// Cloudflare: preferred when the peer is trusted.
		{"CF-Connecting-IP preferred over XFF", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "198.51.100.9", "X-Forwarded-For", "6.6.6.6, 198.51.100.7"), private, "198.51.100.9"},
		{"CF-Connecting-IP IPv6", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "2001:db8::42"), private, "2001:db8::42"},
		{"CF-Connecting-IP invalid falls through to XFF", "172.18.0.5:40000",
			hdr("CF-Connecting-IP", "nope", "X-Forwarded-For", "198.51.100.7"), private, "198.51.100.7"},
		{"CF-Connecting-IP from untrusted peer ignored", "203.0.113.1:1",
			hdr("CF-Connecting-IP", "198.51.100.9"), private, "203.0.113.1"},

		{"IPv6 peer and XFF", "[::1]:8080",
			hdr("X-Forwarded-For", "2001:db8::1"), ParseTrustedProxies([]string{"::1/128"}), "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClientIPFromHeaders(tt.remote, tt.header, tt.trusted); got != tt.want {
				t.Errorf("ClientIPFromHeaders(%q, %v) = %q, want %q", tt.remote, tt.header, got, tt.want)
			}
		})
	}
}

func TestClientIP_Request(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if got := ClientIP(r, DefaultTrustedProxies()); got != "198.51.100.9" {
		t.Errorf("ClientIP = %q, want 198.51.100.9", got)
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
