// Package httputil provides shared HTTP utilities for the web layer.
package httputil

import (
	"net"
	"strings"
)

// ClientIP extracts the real client IP address from a request's RemoteAddr,
// taking into account trusted proxy headers (X-Forwarded-For, X-Real-IP).
//
// Proxy headers are only trusted when the direct connection (RemoteAddr) comes
// from a network listed in trustedProxies. If trustedProxies is empty, only
// RemoteAddr (with port stripped) is returned -- proxy headers are ignored.
func ClientIP(remoteAddr string, xForwardedFor string, xRealIP string, trustedProxies []*net.IPNet) string {
	host := stripPort(remoteAddr)

	// Only trust proxy headers if the direct peer is a trusted proxy
	if len(trustedProxies) > 0 && isTrustedProxy(host, trustedProxies) {
		if xff := xForwardedFor; xff != "" {
			// X-Forwarded-For can contain "client, proxy1, proxy2"
			// The leftmost entry is the original client IP
			if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
				return ip
			}
		}
		if xRealIP != "" {
			return xRealIP
		}
	}

	return host
}

// ParseTrustedProxies parses a slice of CIDR strings into net.IPNet values.
// Single IPs without a mask (e.g. "10.0.0.1") are treated as /32 (IPv4) or /128 (IPv6).
// Invalid entries are silently skipped.
func ParseTrustedProxies(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
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
