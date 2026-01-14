package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ACME CA server URLs.
const (
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// defaultHTTPEntrypoint is the default Traefik entrypoint for HTTP-01 challenge.
	defaultHTTPEntrypoint = "web"
)

// KeyType represents the type of private key for certificates.
type KeyType string

// KeyType constants for certificate private keys.
const (
	KeyTypeRSA2048 KeyType = "RSA2048"
	KeyTypeRSA4096 KeyType = "RSA4096"
	KeyTypeEC256   KeyType = "EC256"
	KeyTypeEC384   KeyType = "EC384"
)

// ACMEConfig contains configuration for ACME certificate resolver.
type ACMEConfig struct {
	// Email is the registration email for Let's Encrypt.
	Email string

	// CAServer is the ACME CA server URL.
	CAServer string

	// Storage is the path to store ACME certificates.
	Storage string

	// KeyType is the type of private key to generate.
	KeyType KeyType

	// DNSProvider for DNS-01 challenge (e.g., "cloudflare", "route53").
	DNSProvider string

	// DNSResolvers are custom DNS servers for DNS propagation checks.
	DNSResolvers []string

	// DNSDelayBefore is the delay before DNS propagation check.
	DNSDelayBefore time.Duration

	// DNSPropagation is the maximum wait time for DNS propagation.
	DNSPropagation time.Duration

	// DisablePropCheck disables DNS propagation checking.
	DisablePropCheck bool

	// TLSChallenge enables TLS-ALPN-01 challenge instead of HTTP-01.
	TLSChallenge bool

	// HTTPChallengeEntryPoint is the entrypoint for HTTP-01 challenge.
	HTTPChallengeEntryPoint string
}

// Domain validation errors.
var (
	ErrDomainEmpty         = errors.New("domain cannot be empty")
	ErrDomainTooLong       = errors.New("domain exceeds maximum length of 253 characters")
	ErrDomainInvalidChars  = errors.New("domain contains invalid characters")
	ErrDomainInvalidFormat = errors.New("domain has invalid format")
	ErrDomainIsIP          = errors.New("IP addresses are not allowed as domains")
	ErrDomainIsLocalhost   = errors.New("localhost is not allowed as domain")
	ErrDomainHasPort       = errors.New("domain should not include port")
	ErrDomainHasProtocol   = errors.New("domain should not include protocol")
	ErrDomainInvalidWild   = errors.New("invalid wildcard domain format")
	ErrACMEEmailRequired   = errors.New("ACME email is required")
	ErrACMEEmailInvalid    = errors.New("ACME email format is invalid")
	ErrACMEStorageRequired = errors.New("ACME storage path is required")
	ErrDomainNotRoutable   = errors.New("domain does not resolve to any IP address")
	ErrDomainNotReachable  = errors.New("domain is not reachable via HTTP")
	ErrDNSRecordNotFound   = errors.New("required DNS record not found")
)

// Validate checks if the ACME configuration is valid.
func (c *ACMEConfig) Validate() error {
	if c.Email == "" {
		return ErrACMEEmailRequired
	}

	// Simple email validation
	if !strings.Contains(c.Email, "@") || !strings.Contains(c.Email, ".") {
		return ErrACMEEmailInvalid
	}

	// Check for proper email format
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(c.Email) {
		return ErrACMEEmailInvalid
	}

	if c.Storage == "" {
		return ErrACMEStorageRequired
	}

	return nil
}

// GenerateTraefikConfig generates Traefik static configuration for ACME.
func (c *ACMEConfig) GenerateTraefikConfig(resolverName string) map[string]string {
	prefix := fmt.Sprintf("certificatesResolvers.%s.acme", resolverName)

	config := map[string]string{
		prefix + ".email":   c.Email,
		prefix + ".storage": c.Storage,
	}

	if c.CAServer != "" {
		config[prefix+".caServer"] = c.CAServer
	}

	if c.KeyType != "" {
		config[prefix+".keyType"] = string(c.KeyType)
	}

	// Challenge configuration (mutually exclusive)
	switch {
	case c.DNSProvider != "":
		// DNS-01 challenge
		config[prefix+".dnsChallenge.provider"] = c.DNSProvider

		if len(c.DNSResolvers) > 0 {
			config[prefix+".dnsChallenge.resolvers"] = strings.Join(c.DNSResolvers, ",")
		}

		if c.DNSDelayBefore > 0 {
			config[prefix+".dnsChallenge.delayBeforeCheck"] = c.DNSDelayBefore.String()
		}

		if c.DNSPropagation > 0 {
			config[prefix+".dnsChallenge.propagationTimeout"] = c.DNSPropagation.String()
		}

		if c.DisablePropCheck {
			config[prefix+".dnsChallenge.disablePropagationCheck"] = labelTrue
		}
	case c.TLSChallenge:
		// TLS-ALPN-01 challenge
		config[prefix+".tlsChallenge"] = labelTrue
	default:
		// HTTP-01 challenge (default)
		entryPoint := c.HTTPChallengeEntryPoint
		if entryPoint == "" {
			entryPoint = defaultHTTPEntrypoint
		}
		config[prefix+".httpChallenge.entryPoint"] = entryPoint
	}

	return config
}

// DefaultACMEConfig returns a default ACME configuration for production.
func DefaultACMEConfig(email string) ACMEConfig {
	return ACMEConfig{
		Email:    email,
		CAServer: LetsEncryptProduction,
		Storage:  "/data/acme.json",
		KeyType:  KeyTypeRSA4096,
	}
}

// StagingACMEConfig returns an ACME configuration for Let's Encrypt staging.
func StagingACMEConfig(email string) ACMEConfig {
	return ACMEConfig{
		Email:    email,
		CAServer: LetsEncryptStaging,
		Storage:  "/data/acme.json",
		KeyType:  KeyTypeRSA4096,
	}
}

// DomainValidator provides domain validation and verification.
type DomainValidator struct {
	resolver    *net.Resolver
	httpClient  *http.Client
	httpTimeout time.Duration
}

// NewDomainValidator creates a new domain validator with default settings.
func NewDomainValidator() *DomainValidator {
	return &DomainValidator{
		resolver: net.DefaultResolver,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		httpTimeout: 10 * time.Second,
	}
}

// NewDomainValidatorWithResolver creates a validator with a custom DNS resolver.
func NewDomainValidatorWithResolver(resolver *net.Resolver) *DomainValidator {
	v := NewDomainValidator()
	v.resolver = resolver
	return v
}

// ValidateDomain validates that a domain is properly formatted.
func (v *DomainValidator) ValidateDomain(domain string) error {
	if domain == "" {
		return ErrDomainEmpty
	}

	// Check for protocol
	if strings.Contains(domain, "://") {
		return ErrDomainHasProtocol
	}

	// Check for port
	if strings.Contains(domain, ":") {
		return ErrDomainHasPort
	}

	// Check for wildcard (use ValidateWildcard for wildcards)
	if strings.HasPrefix(domain, "*") {
		return ErrDomainInvalidWild
	}

	// Normalize
	domain = v.NormalizeDomain(domain)

	// Length check
	if len(domain) > 253 {
		return ErrDomainTooLong
	}

	// Check for IP address
	if net.ParseIP(domain) != nil {
		return ErrDomainIsIP
	}

	// Check for localhost
	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		return ErrDomainIsLocalhost
	}

	// Validate domain format
	if !isValidDomainFormat(domain) {
		return ErrDomainInvalidFormat
	}

	return nil
}

// ValidateWildcard validates a wildcard domain format.
func (v *DomainValidator) ValidateWildcard(domain string) error {
	if !strings.HasPrefix(domain, "*.") {
		return ErrDomainInvalidWild
	}

	// Extract base domain after wildcard
	baseDomain := strings.TrimPrefix(domain, "*.")

	if baseDomain == "" {
		return ErrDomainInvalidWild
	}

	// The base domain should be valid
	return v.ValidateDomain(baseDomain)
}

// IsDomainRoutable checks if the domain resolves to any IP address.
func (v *DomainValidator) IsDomainRoutable(ctx context.Context, domain string) (bool, error) {
	domain = v.NormalizeDomain(domain)

	addrs, err := v.resolver.LookupHost(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, nil
		}
		return false, fmt.Errorf("DNS lookup failed: %w", err)
	}

	return len(addrs) > 0, nil
}

// VerifyDNSRecord checks if a specific DNS record type exists.
func (v *DomainValidator) VerifyDNSRecord(ctx context.Context, domain, recordType string) (bool, error) {
	domain = v.NormalizeDomain(domain)

	switch strings.ToUpper(recordType) {
	case "A":
		addrs, err := v.resolver.LookupIP(ctx, "ip4", domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return len(addrs) > 0, nil

	case "AAAA":
		addrs, err := v.resolver.LookupIP(ctx, "ip6", domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return len(addrs) > 0, nil

	case "CNAME":
		cname, err := v.resolver.LookupCNAME(ctx, domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return cname != "", nil

	case "TXT":
		records, err := v.resolver.LookupTXT(ctx, domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return len(records) > 0, nil

	case "MX":
		records, err := v.resolver.LookupMX(ctx, domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return len(records) > 0, nil

	case "NS":
		records, err := v.resolver.LookupNS(ctx, domain)
		if err != nil {
			return false, handleDNSError(err)
		}
		return len(records) > 0, nil

	default:
		return false, fmt.Errorf("unsupported DNS record type: %s", recordType)
	}
}

// CheckHTTPReachability verifies that the domain is reachable via HTTP.
func (v *DomainValidator) CheckHTTPReachability(ctx context.Context, domain string, port int) (bool, error) {
	domain = v.NormalizeDomain(domain)

	url := fmt.Sprintf("http://%s:%d/", domain, port)
	if port == 80 {
		url = fmt.Sprintf("http://%s/", domain)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Any response (even 4xx/5xx) means the server is reachable
	return true, nil
}

// NormalizeDomain normalizes a domain name to lowercase and trims whitespace.
func (v *DomainValidator) NormalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.ToLower(domain)
	domain = strings.TrimSuffix(domain, ".")
	return domain
}

// ExtractBaseDomain extracts the base domain from a subdomain.
// Note: This is a simple implementation that may not handle all TLDs correctly.
func (v *DomainValidator) ExtractBaseDomain(domain string) string {
	domain = v.NormalizeDomain(domain)
	domain = strings.TrimPrefix(domain, "*.")

	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}

	// Simple heuristic: if second-to-last part is very short (like "co"),
	// assume it's part of a two-part TLD (like "co.uk")
	if len(parts) >= 3 && len(parts[len(parts)-2]) <= 3 {
		return strings.Join(parts[len(parts)-3:], ".")
	}

	return strings.Join(parts[len(parts)-2:], ".")
}

// isValidDomainFormat checks if the domain has valid format.
func isValidDomainFormat(domain string) bool {
	// Domain regex: allows alphanumeric characters and hyphens
	// Each label must start and end with alphanumeric
	domainRegex := regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

	if !domainRegex.MatchString(domain) {
		return false
	}

	// Check each label length (max 63 characters)
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 {
			return false
		}
	}

	// Must have at least one dot (TLD)
	return strings.Contains(domain, ".")
}

// handleDNSError converts DNS errors to appropriate responses.
func handleDNSError(err error) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return nil // Not found is not an error, just means record doesn't exist
		}
		if dnsErr.IsTimeout {
			return fmt.Errorf("DNS lookup timed out: %w", err)
		}
	}
	return err
}

// VerificationResult contains the results of domain verification.
type VerificationResult struct {
	Domain      string
	IsValid     bool
	IsRoutable  bool
	HasARecord  bool
	HasAAAA     bool
	IsReachable bool
	Errors      []string
}

// VerifyDomainFull performs comprehensive domain verification.
func (v *DomainValidator) VerifyDomainFull(ctx context.Context, domain string) *VerificationResult {
	result := &VerificationResult{
		Domain: domain,
	}

	// Validate format
	if err := v.ValidateDomain(domain); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.IsValid = true

	domain = v.NormalizeDomain(domain)

	// Check if domain is routable
	routable, err := v.IsDomainRoutable(ctx, domain)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("DNS check failed: %v", err))
	} else {
		result.IsRoutable = routable
	}

	// Check for A record
	hasA, err := v.VerifyDNSRecord(ctx, domain, "A")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("A record check failed: %v", err))
	} else {
		result.HasARecord = hasA
	}

	// Check for AAAA record
	hasAAAA, err := v.VerifyDNSRecord(ctx, domain, "AAAA")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("AAAA record check failed: %v", err))
	} else {
		result.HasAAAA = hasAAAA
	}

	// Check HTTP reachability
	reachable, err := v.CheckHTTPReachability(ctx, domain, 80)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("HTTP check failed: %v", err))
	} else {
		result.IsReachable = reachable
	}

	return result
}
