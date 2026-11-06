package proxy

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestACMEConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ACMEConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ACMEConfig{
				Email:       "admin@example.com",
				CAServer:    LetsEncryptProduction,
				Storage:     "/data/acme.json",
				KeyType:     KeyTypeRSA4096,
				DNSProvider: "",
			},
			wantErr: false,
		},
		{
			name: "missing email",
			config: ACMEConfig{
				CAServer: LetsEncryptProduction,
				Storage:  "/data/acme.json",
			},
			wantErr: true,
		},
		{
			name: "invalid email",
			config: ACMEConfig{
				Email:    "not-an-email",
				CAServer: LetsEncryptProduction,
				Storage:  "/data/acme.json",
			},
			wantErr: true,
		},
		{
			name: "empty storage path",
			config: ACMEConfig{
				Email:    "admin@example.com",
				CAServer: LetsEncryptProduction,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ACMEConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestACMEConfig_GenerateTraefikConfig(t *testing.T) {
	cfg := ACMEConfig{
		Email:    "admin@example.com",
		CAServer: LetsEncryptStaging,
		Storage:  "/data/acme.json",
		KeyType:  KeyTypeEC384,
	}

	config := cfg.GenerateTraefikConfig("letsencrypt")

	if config["certificatesResolvers.letsencrypt.acme.email"] != "admin@example.com" {
		t.Error("expected email to be set")
	}
	if config["certificatesResolvers.letsencrypt.acme.caServer"] != LetsEncryptStaging {
		t.Error("expected staging CA server")
	}
	if config["certificatesResolvers.letsencrypt.acme.storage"] != "/data/acme.json" {
		t.Error("expected storage path to be set")
	}
	if config["certificatesResolvers.letsencrypt.acme.keyType"] != string(KeyTypeEC384) {
		t.Error("expected key type to be set")
	}
}

func TestACMEConfig_GenerateTraefikConfig_HTTPChallenge(t *testing.T) {
	cfg := ACMEConfig{
		Email:    "admin@example.com",
		CAServer: LetsEncryptProduction,
		Storage:  "/data/acme.json",
	}

	config := cfg.GenerateTraefikConfig("letsencrypt")

	// Should default to HTTP challenge
	if config["certificatesResolvers.letsencrypt.acme.httpChallenge.entryPoint"] != "web" {
		t.Error("expected HTTP challenge with web entrypoint")
	}
}

func TestACMEConfig_GenerateTraefikConfig_TLSChallenge(t *testing.T) {
	cfg := ACMEConfig{
		Email:        "admin@example.com",
		CAServer:     LetsEncryptProduction,
		Storage:      "/data/acme.json",
		TLSChallenge: true,
	}

	config := cfg.GenerateTraefikConfig("letsencrypt")

	// Should use TLS-ALPN challenge
	if _, ok := config["certificatesResolvers.letsencrypt.acme.tlsChallenge"]; !ok {
		t.Error("expected TLS challenge to be set")
	}
	// Should not have HTTP challenge
	if _, ok := config["certificatesResolvers.letsencrypt.acme.httpChallenge.entryPoint"]; ok {
		t.Error("expected HTTP challenge to not be set when TLS challenge is enabled")
	}
}

func TestACMEConfig_GenerateTraefikConfig_DNSChallenge(t *testing.T) {
	cfg := ACMEConfig{
		Email:            "admin@example.com",
		CAServer:         LetsEncryptProduction,
		Storage:          "/data/acme.json",
		DNSProvider:      "cloudflare",
		DNSResolvers:     []string{"1.1.1.1:53", "8.8.8.8:53"},
		DNSDelayBefore:   10 * time.Second,
		DNSPropagation:   60 * time.Second,
		DisablePropCheck: false,
	}

	config := cfg.GenerateTraefikConfig("letsencrypt")

	if config["certificatesResolvers.letsencrypt.acme.dnsChallenge.provider"] != "cloudflare" {
		t.Error("expected DNS provider to be set")
	}
	if config["certificatesResolvers.letsencrypt.acme.dnsChallenge.resolvers"] != "1.1.1.1:53,8.8.8.8:53" {
		t.Error("expected DNS resolvers to be set")
	}
	if config["certificatesResolvers.letsencrypt.acme.dnsChallenge.delayBeforeCheck"] != "10s" {
		t.Error("expected delay before check to be set")
	}
}

func TestDefaultACMEConfig(t *testing.T) {
	cfg := DefaultACMEConfig("admin@example.com")

	if cfg.Email != "admin@example.com" {
		t.Errorf("expected email %q, got %q", "admin@example.com", cfg.Email)
	}
	if cfg.CAServer != LetsEncryptProduction {
		t.Errorf("expected production CA, got %q", cfg.CAServer)
	}
	if cfg.Storage != "/data/acme.json" {
		t.Errorf("expected default storage path, got %q", cfg.Storage)
	}
	if cfg.KeyType != KeyTypeRSA4096 {
		t.Errorf("expected RSA4096 key type, got %q", cfg.KeyType)
	}
}

func TestStagingACMEConfig(t *testing.T) {
	cfg := StagingACMEConfig("admin@example.com")

	if cfg.CAServer != LetsEncryptStaging {
		t.Errorf("expected staging CA, got %q", cfg.CAServer)
	}
}

func TestDomainValidator_ValidateDomain(t *testing.T) {
	v := NewDomainValidator()

	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid domain", "example.com", false},
		{"valid subdomain", "app.example.com", false},
		{"valid multi-level subdomain", "api.v1.example.com", false},
		{"empty domain", "", true},
		{"too long domain", string(make([]byte, 256)), true},
		{"invalid characters", "exam ple.com", true},
		{"starts with hyphen", "-example.com", true},
		{"ends with hyphen", "example-.com", true},
		{"double dots", "example..com", true},
		{"ip address", "192.168.1.1", true},
		{"localhost", "localhost", true},
		{"with port", "example.com:8080", true},
		{"with protocol", "https://example.com", true},
		{"wildcard", "*.example.com", true},
		{"underscore", "ex_ample.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomain(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

func TestDomainValidator_ValidateWildcard(t *testing.T) {
	v := NewDomainValidator()

	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid wildcard", "*.example.com", false},
		{"non-wildcard", "example.com", true},
		{"invalid wildcard position", "app.*.example.com", true},
		{"wildcard only", "*", true},
		{"wildcard with subdomain", "*.app.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateWildcard(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWildcard(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

// newTestResolver serves deterministic DNS answers on loopback. Tests never
// depend on public DNS or on a name belonging to somebody else.
func newTestResolver(t *testing.T, exchange func(question string) (uint16, []net.IP, error)) *net.Resolver {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		defer func() { _ = conn.Close() }()
		for {
			buf := make([]byte, 512)
			n, peer, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				return
			}
			if n < 12 {
				continue
			}
			question := ""
			at := 12
			for at < n {
				length := int(buf[at])
				at++
				if length == 0 {
					break
				}
				if at+length > n {
					break
				}
				if question != "" {
					question += "."
				}
				question += string(buf[at : at+length])
				at += length
			}
			if at+2 > n {
				continue
			}
			qtype := uint16(buf[at])<<8 | uint16(buf[at+1])
			questionEnd := at + 4 // QTYPE and QCLASS; do not copy EDNS records.
			rcode, addrs, exchangeErr := exchange(question)
			if exchangeErr != nil {
				continue
			}
			answer := append([]byte(nil), buf[:questionEnd]...)
			answer[2] = 0x85
			answer[3] = 0x80 | byte(rcode)
			answer[6], answer[7] = 0, 0
			answer[8], answer[9], answer[10], answer[11] = 0, 0, 0, 0
			if rcode == 0 && qtype == 1 {
				for _, ip := range addrs {
					if v4 := ip.To4(); v4 != nil {
						answer[7]++
						answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4)
						answer = append(answer, v4...)
					}
				}
			}
			if _, writeErr := conn.WriteTo(answer, peer); writeErr != nil {
				return
			}
		}
	}()
	_, portText, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		},
	}
}

func TestDomainValidator_IsDomainRoutable(t *testing.T) {
	resolver := newTestResolver(t, func(question string) (uint16, []net.IP, error) {
		switch question {
		case "app.example.test":
			return 0, []net.IP{net.ParseIP("192.0.2.10")}, nil
		case "empty.example.test":
			return 0, nil, nil
		case "timeout.example.test":
			return 0, nil, context.DeadlineExceeded
		default:
			return 3, nil, nil
		}
	})
	v := NewDomainValidatorWithResolver(resolver)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	for _, tc := range []struct {
		name, domain string
		want         bool
		wantErr      bool
	}{
		{"address", " APP.EXAMPLE.TEST. ", true, false},
		{"nxdomain", "missing.example.test", false, false},
		{"no address", "empty.example.test", false, false},
		{"resolver timeout", "timeout.example.test", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := v.IsDomainRoutable(ctx, tc.domain)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("IsDomainRoutable(%q) = %v, %v; want %v, error=%v", tc.domain, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestDomainValidator_VerifyDNSRecord(t *testing.T) {
	resolver := newTestResolver(t, func(question string) (uint16, []net.IP, error) {
		if question == "app.example.test" {
			return 0, []net.IP{net.ParseIP("192.0.2.10")}, nil
		}
		return 3, nil, nil
	})
	v := NewDomainValidatorWithResolver(resolver)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	hasRecord, err := v.VerifyDNSRecord(ctx, "app.example.test", "A")
	if err != nil || !hasRecord {
		t.Fatalf("A record = %v, %v; want true, nil", hasRecord, err)
	}
	hasRecord, err = v.VerifyDNSRecord(ctx, "missing.example.test", "A")
	if err != nil || hasRecord {
		t.Fatalf("NXDOMAIN record = %v, %v; want false, nil", hasRecord, err)
	}
}

func TestDomainValidator_CheckHTTPReachability(t *testing.T) {
	v := NewDomainValidator()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test with a known reachable site
	reachable, err := v.CheckHTTPReachability(ctx, "example.com", 80)
	if err != nil {
		t.Skipf("HTTP check failed (network issue?): %v", err)
	}
	if !reachable {
		t.Error("expected example.com:80 to be reachable")
	}
}

func TestDomainValidator_NormalizeDomain(t *testing.T) {
	v := NewDomainValidator()

	tests := []struct {
		input string
		want  string
	}{
		{"EXAMPLE.COM", "example.com"},
		{"Example.Com", "example.com"},
		{"  example.com  ", "example.com"},
		{"example.com.", "example.com"},
		{"EXAMPLE.COM.", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.NormalizeDomain(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDomainValidator_ExtractBaseDomain(t *testing.T) {
	v := NewDomainValidator()

	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "example.com"},
		{"app.example.com", "example.com"},
		{"api.v1.example.com", "example.com"},
		{"*.example.com", "example.com"},
		{"co.uk", "co.uk"},
		// Note: app.co.uk is complex because co.uk is a public suffix
		// Our simple implementation returns app.co.uk which is reasonable
		// for most use cases. Full PSL support would require a dependency.
		{"app.co.uk", "app.co.uk"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := v.ExtractBaseDomain(tt.domain)
			if got != tt.want {
				t.Errorf("ExtractBaseDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestDomainValidation_IsIPAddress(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"::1", true},
		{"2001:db8::1", true},
		{"example.com", false},
		{"192.168.1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := net.ParseIP(tt.input) != nil
			if got != tt.want {
				t.Errorf("IsIPAddress(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestKeyType_String(t *testing.T) {
	tests := []struct {
		keyType KeyType
		want    string
	}{
		{KeyTypeRSA2048, "RSA2048"},
		{KeyTypeRSA4096, "RSA4096"},
		{KeyTypeEC256, "EC256"},
		{KeyTypeEC384, "EC384"},
	}

	for _, tt := range tests {
		t.Run(string(tt.keyType), func(t *testing.T) {
			if string(tt.keyType) != tt.want {
				t.Errorf("KeyType = %q, want %q", tt.keyType, tt.want)
			}
		})
	}
}
