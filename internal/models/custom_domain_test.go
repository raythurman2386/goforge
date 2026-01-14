package models

import (
	"database/sql"
	"testing"
	"time"
)

func TestCustomDomain_IsVerified(t *testing.T) {
	tests := []struct {
		name   string
		domain CustomDomain
		want   bool
	}{
		{
			name: "verified domain",
			domain: CustomDomain{
				Status:     DomainStatusActive,
				VerifiedAt: sql.NullTime{Time: time.Now(), Valid: true},
			},
			want: true,
		},
		{
			name: "pending domain",
			domain: CustomDomain{
				Status: DomainStatusPending,
			},
			want: false,
		},
		{
			name: "active but not verified",
			domain: CustomDomain{
				Status: DomainStatusActive,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.IsVerified(); got != tt.want {
				t.Errorf("IsVerified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_HasSSL(t *testing.T) {
	tests := []struct {
		name   string
		domain CustomDomain
		want   bool
	}{
		{
			name:   "active SSL",
			domain: CustomDomain{SSLStatus: SSLStatusActive},
			want:   true,
		},
		{
			name:   "pending SSL",
			domain: CustomDomain{SSLStatus: SSLStatusPending},
			want:   false,
		},
		{
			name:   "no SSL",
			domain: CustomDomain{SSLStatus: SSLStatusNone},
			want:   false,
		},
		{
			name:   "expired SSL",
			domain: CustomDomain{SSLStatus: SSLStatusExpired},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.HasSSL(); got != tt.want {
				t.Errorf("HasSSL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_IsSSLExpiringSoon(t *testing.T) {
	tests := []struct {
		name   string
		domain CustomDomain
		within time.Duration
		want   bool
	}{
		{
			name: "expiring soon",
			domain: CustomDomain{
				SSLExpiresAt: sql.NullTime{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
			},
			within: 30 * 24 * time.Hour,
			want:   true,
		},
		{
			name: "not expiring soon",
			domain: CustomDomain{
				SSLExpiresAt: sql.NullTime{Time: time.Now().Add(60 * 24 * time.Hour), Valid: true},
			},
			within: 30 * 24 * time.Hour,
			want:   false,
		},
		{
			name:   "no expiration date",
			domain: CustomDomain{},
			within: 30 * 24 * time.Hour,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.IsSSLExpiringSoon(tt.within); got != tt.want {
				t.Errorf("IsSSLExpiringSoon() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_NeedsVerification(t *testing.T) {
	tests := []struct {
		name   string
		status DomainStatus
		want   bool
	}{
		{"pending", DomainStatusPending, true},
		{"verifying", DomainStatusVerifying, true},
		{"active", DomainStatusActive, false},
		{"failed", DomainStatusFailed, false},
		{"expired", DomainStatusExpired, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := CustomDomain{Status: tt.status}
			if got := d.NeedsVerification(); got != tt.want {
				t.Errorf("NeedsVerification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_CanRetryVerification(t *testing.T) {
	tests := []struct {
		name        string
		domain      CustomDomain
		maxAttempts int
		want        bool
	}{
		{
			name:        "can retry - under max",
			domain:      CustomDomain{Status: DomainStatusFailed, CheckAttempts: 2},
			maxAttempts: 5,
			want:        true,
		},
		{
			name:        "cannot retry - at max",
			domain:      CustomDomain{Status: DomainStatusFailed, CheckAttempts: 5},
			maxAttempts: 5,
			want:        false,
		},
		{
			name:        "cannot retry - already active",
			domain:      CustomDomain{Status: DomainStatusActive, CheckAttempts: 1},
			maxAttempts: 5,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.CanRetryVerification(tt.maxAttempts); got != tt.want {
				t.Errorf("CanRetryVerification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_GetVerificationDNSRecord(t *testing.T) {
	d := CustomDomain{Domain: "example.com"}
	want := "_gocool-verify.example.com"
	if got := d.GetVerificationDNSRecord(); got != want {
		t.Errorf("GetVerificationDNSRecord() = %v, want %v", got, want)
	}
}

func TestCustomDomain_RedirectsWWW(t *testing.T) {
	tests := []struct {
		name   string
		domain CustomDomain
		want   bool
	}{
		{
			name:   "redirects www",
			domain: CustomDomain{WWWRedirect: sql.NullString{String: "www", Valid: true}},
			want:   true,
		},
		{
			name:   "redirects apex",
			domain: CustomDomain{WWWRedirect: sql.NullString{String: "apex", Valid: true}},
			want:   false,
		},
		{
			name:   "no redirect",
			domain: CustomDomain{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.RedirectsWWW(); got != tt.want {
				t.Errorf("RedirectsWWW() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomain_RedirectsApex(t *testing.T) {
	tests := []struct {
		name   string
		domain CustomDomain
		want   bool
	}{
		{
			name:   "redirects apex",
			domain: CustomDomain{WWWRedirect: sql.NullString{String: "apex", Valid: true}},
			want:   true,
		},
		{
			name:   "redirects www",
			domain: CustomDomain{WWWRedirect: sql.NullString{String: "www", Valid: true}},
			want:   false,
		},
		{
			name:   "no redirect",
			domain: CustomDomain{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.domain.RedirectsApex(); got != tt.want {
				t.Errorf("RedirectsApex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDomainStatus_Values(t *testing.T) {
	// Verify all status values are distinct
	statuses := []DomainStatus{
		DomainStatusPending,
		DomainStatusVerifying,
		DomainStatusActive,
		DomainStatusFailed,
		DomainStatusExpired,
	}

	seen := make(map[DomainStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status value: %v", s)
		}
		seen[s] = true
	}
}

func TestSSLStatus_Values(t *testing.T) {
	// Verify all SSL status values are distinct
	statuses := []SSLStatus{
		SSLStatusNone,
		SSLStatusPending,
		SSLStatusActive,
		SSLStatusExpiring,
		SSLStatusExpired,
		SSLStatusFailed,
	}

	seen := make(map[SSLStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate SSL status value: %v", s)
		}
		seen[s] = true
	}
}
