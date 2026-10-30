// Package httputil provides shared HTTP utilities for the web layer.
package httputil

import (
	"net"
	"net/http"
	"strings"
)

// Header names consulted by ClientIP. They are only read when the direct
// peer (RemoteAddr) is a trusted proxy.
const (
	headerCFConnectingIP = "CF-Connecting-IP"
	headerXForwardedFor  = "X-Forwarded-For"
	headerXRealIP        = "X-Real-IP"
)

// ClientIP returns the client IP for r. See ClientIPFromHeaders.
func ClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	return ClientIPFromHeaders(r.RemoteAddr, r.Header, trustedProxies)
}

// ClientIPFromHeaders extracts the real client IP address.
//
// Proxy headers are only read when the direct peer (remoteAddr) is inside
// trustedProxies. Otherwise, or when trustedProxies is empty, the peer address
// (port stripped) is returned and every proxy header is ignored.
//
// From a trusted peer the order is:
//
//  1. CF-Connecting-IP, when it holds a valid IP. Cloudflare's edge overwrites
//     this header, so a client cannot choose it when traffic arrives through
//     Cloudflare (Tunnel or proxy).
//  2. X-Forwarded-For, walked right to left: the first hop that is not a
//     trusted proxy is the client. Every proxy appends the address it saw, so
//     only the rightmost untrusted entry is known to be real; anything left of
//     it was written by the client and can be spoofed. If a hop does not parse
//     as an IP, the walk stops and the last trusted hop is returned. If every
//     hop is trusted, the leftmost hop is returned.
//  3. X-Real-IP, when it holds a valid IP.
//  4. The peer address.
func ClientIPFromHeaders(remoteAddr string, h http.Header, trustedProxies []*net.IPNet) string {
	peer := stripPort(remoteAddr)
	if len(trustedProxies) == 0 || !isTrustedProxy(peer, trustedProxies) {
		return peer
	}

	if ip := parseIP(h.Get(headerCFConnectingIP)); ip != "" {
		return ip
	}

	if ip, ok := rightmostUntrusted(h.Values(headerXForwardedFor), peer, trustedProxies); ok {
		return ip
	}

	if ip := parseIP(h.Get(headerXRealIP)); ip != "" {
		return ip
	}

	return peer
}

// rightmostUntrusted walks every X-Forwarded-For hop (all header lines, in
// order) from the right and returns the first hop that is not a trusted proxy.
// ok is false when the header is absent or has no hops.
func rightmostUntrusted(values []string, peer string, trusted []*net.IPNet) (string, bool) {
	var hops []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				hops = append(hops, part)
			}
		}
	}
	if len(hops) == 0 {
		return "", false
	}

	last := peer // closest hop known to be real
	for i := len(hops) - 1; i >= 0; i-- {
		ip := parseIP(hops[i])
		if ip == "" {
			// Garbage left of a trusted hop came from the client. Do not
			// let it become a rate-limit key; fall back to the last real hop.
			return last, true
		}
		if !isTrustedProxy(ip, trusted) {
			return ip, true
		}
		last = ip
	}
	// Every hop is a trusted proxy (internal traffic); the leftmost is the origin.
	return last, true
}

// parseIP returns the canonical form of s if it is an IP address, or "".
// A bracketed or port-suffixed value ("[::1]:80", "1.2.3.4:80") is accepted.
func parseIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
	}
	return ""
}

// ParseTrustedProxies parses a slice of CIDR strings into net.IPNet values.
// Single IPs without a mask (e.g. "10.0.0.1") are treated as /32 (IPv4) or /128 (IPv6).
// Invalid entries are silently skipped.
func ParseTrustedProxies(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		// If it's a plain IP without CIDR notation, add the appropriate mask
		if !strings.Contains(cidr, "/") {
			if strings.Contains(cidr, ":") {
				cidr += "/128"
			} else {
				cidr += "/32"
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// DefaultTrustedProxies returns CIDRs for common private/loopback networks.
// This is suitable when GoForge runs behind a local reverse proxy like Traefik.
func DefaultTrustedProxies() []*net.IPNet {
	return ParseTrustedProxies([]string{
		"127.0.0.0/8",    // IPv4 loopback
		"::1/128",        // IPv6 loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"fc00::/7",       // IPv6 unique local
	})
}

func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// RemoteAddr might not have a port
		return addr
	}
	return host
}

func isTrustedProxy(ip string, trusted []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range trusted {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}
