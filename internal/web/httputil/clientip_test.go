package httputil

import (
	"net"
	"testing"
)

func TestClientIP_NoTrustedProxies(t *testing.T) {
	// With no trusted proxies, proxy headers are ignored
	ip := ClientIP("1.2.3.4:5678", "9.9.9.9", "8.8.8.8", nil)
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", ip)
	}
}

func TestClientIP_TrustedProxy_XForwardedFor(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	ip := ClientIP("10.0.0.1:1234", "203.0.113.50, 10.0.0.1", "", trusted)
	if ip != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", ip)
	}
}

func TestClientIP_TrustedProxy_XRealIP(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	ip := ClientIP("10.0.0.1:1234", "", "203.0.113.50", trusted)
	if ip != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", ip)
	}
}

func TestClientIP_UntrustedProxy_IgnoresHeaders(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	// Peer is not in trusted range, so headers are ignored
	ip := ClientIP("203.0.113.99:5678", "1.1.1.1", "2.2.2.2", trusted)
	if ip != "203.0.113.99" {
		t.Errorf("expected 203.0.113.99, got %s", ip)
	}
}

func TestClientIP_XForwardedFor_MultipleIPs(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"127.0.0.1/32"})

	ip := ClientIP("127.0.0.1:8080", "5.6.7.8, 10.0.0.1, 127.0.0.1", "", trusted)
	if ip != "5.6.7.8" {
		t.Errorf("expected 5.6.7.8, got %s", ip)
	}
}

func TestClientIP_XForwardedFor_Whitespace(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"127.0.0.1/32"})

	ip := ClientIP("127.0.0.1:8080", "  5.6.7.8 , 10.0.0.1", "", trusted)
	if ip != "5.6.7.8" {
		t.Errorf("expected 5.6.7.8, got %s", ip)
	}
}

func TestClientIP_NoPort(t *testing.T) {
	ip := ClientIP("1.2.3.4", "", "", nil)
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", ip)
	}
}

func TestClientIP_IPv6(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"::1/128"})

	ip := ClientIP("[::1]:8080", "2001:db8::1", "", trusted)
	if ip != "2001:db8::1" {
		t.Errorf("expected 2001:db8::1, got %s", ip)
	}
}

func TestClientIP_EmptyHeaders_ReturnsPeer(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	ip := ClientIP("10.0.0.1:1234", "", "", trusted)
	if ip != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", ip)
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
