package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// DomainValidator checks whether a domain resolves without making an HTTP request.
type DomainValidator interface {
	IsDomainRoutable(ctx context.Context, domain string) (bool, error)
}

type domainValidationError struct {
	status  int
	message string
}

func (e *domainValidationError) Error() string { return e.message }

func validateDomainDNS(ctx context.Context, validator DomainValidator, domain, field string) (string, *domainValidationError) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return "", nil
	}
	if !isValidDomain(domain) {
		return "", &domainValidationError{status: http.StatusBadRequest, message: "invalid " + field + " format"}
	}
	if validator == nil {
		return "", &domainValidationError{status: http.StatusInternalServerError, message: "domain DNS validation is not configured"}
	}
	routable, err := validator.IsDomainRoutable(ctx, domain)
	if err != nil {
		return "", &domainValidationError{
			status:  http.StatusServiceUnavailable,
			message: fmt.Sprintf("could not check DNS for %s %q; check the DNS resolver and try again", field, domain),
		}
	}
	if !routable {
		return "", &domainValidationError{
			status:  http.StatusBadRequest,
			message: fmt.Sprintf("%s %q does not resolve to an IP address; create a DNS record and try again", field, domain),
		}
	}
	return domain, nil
}
