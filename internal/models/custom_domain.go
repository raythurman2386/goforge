package models

import (
	"database/sql"
	"time"
)

// DomainStatus represents the verification status of a custom domain.
type DomainStatus string

const (
	// DomainStatusPending indicates the domain is awaiting verification.
	DomainStatusPending DomainStatus = "pending"
	// DomainStatusVerifying indicates DNS verification is in progress.
	DomainStatusVerifying DomainStatus = "verifying"
	// DomainStatusActive indicates the domain is verified and active.
	DomainStatusActive DomainStatus = "active"
	// DomainStatusFailed indicates domain verification failed.
	DomainStatusFailed DomainStatus = "failed"
	// DomainStatusExpired indicates the domain verification has expired.
	DomainStatusExpired DomainStatus = "expired"
)

// SSLStatus represents the SSL certificate status for a domain.
type SSLStatus string

const (
	// SSLStatusNone indicates no SSL certificate.
	SSLStatusNone SSLStatus = "none"
	// SSLStatusPending indicates SSL certificate is being provisioned.
	SSLStatusPending SSLStatus = "pending"
	// SSLStatusActive indicates SSL certificate is active.
	SSLStatusActive SSLStatus = "active"
	// SSLStatusExpiring indicates SSL certificate is expiring soon.
	SSLStatusExpiring SSLStatus = "expiring"
	// SSLStatusExpired indicates SSL certificate has expired.
	SSLStatusExpired SSLStatus = "expired"
	// SSLStatusFailed indicates SSL certificate provisioning failed.
	SSLStatusFailed SSLStatus = "failed"
)

// CustomDomain represents a custom domain configuration for a project environment.
type CustomDomain struct {
	ID            string
	EnvironmentID string
	Domain        string
	Status        DomainStatus
	SSLStatus     SSLStatus

	// Verification fields
	VerificationToken  string         // Token for DNS TXT record verification
	VerificationRecord string         // Expected DNS record value
	VerifiedAt         sql.NullTime   // When the domain was verified
	LastCheckAt        sql.NullTime   // When the last verification check was performed
	CheckAttempts      int            // Number of verification attempts
	FailureReason      sql.NullString // Reason for verification failure

	// SSL fields
	SSLExpiresAt     sql.NullTime   // When the SSL certificate expires
	SSLProvisionedAt sql.NullTime   // When SSL was last provisioned
	SSLFailureReason sql.NullString // Reason for SSL provisioning failure

	// Configuration
	Primary     bool           // Is this the primary domain for the environment
	ForceHTTPS  bool           // Force HTTPS redirect
	WWWRedirect sql.NullString // "www" to redirect www to apex, "apex" to redirect apex to www, null for none

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsVerified returns true if the domain has been verified.
func (d *CustomDomain) IsVerified() bool {
	return d.Status == DomainStatusActive && d.VerifiedAt.Valid
}

// HasSSL returns true if the domain has an active SSL certificate.
func (d *CustomDomain) HasSSL() bool {
	return d.SSLStatus == SSLStatusActive
}

// IsSSLExpiringSoon returns true if SSL certificate expires within the given duration.
func (d *CustomDomain) IsSSLExpiringSoon(within time.Duration) bool {
	if !d.SSLExpiresAt.Valid {
		return false
	}
	return time.Until(d.SSLExpiresAt.Time) < within
}

// NeedsVerification returns true if the domain needs verification.
func (d *CustomDomain) NeedsVerification() bool {
	return d.Status == DomainStatusPending || d.Status == DomainStatusVerifying
}

// CanRetryVerification returns true if verification can be retried.
func (d *CustomDomain) CanRetryVerification(maxAttempts int) bool {
	if d.Status == DomainStatusActive {
		return false
	}
	return d.CheckAttempts < maxAttempts
}

// GetVerificationDNSRecord returns the expected DNS TXT record for verification.
func (d *CustomDomain) GetVerificationDNSRecord() string {
	return "_gocool-verify." + d.Domain
}

// RedirectsWWW returns true if this domain redirects www subdomain.
func (d *CustomDomain) RedirectsWWW() bool {
	return d.WWWRedirect.Valid && d.WWWRedirect.String == "www"
}

// RedirectsApex returns true if this domain redirects apex to www.
func (d *CustomDomain) RedirectsApex() bool {
	return d.WWWRedirect.Valid && d.WWWRedirect.String == "apex"
}
