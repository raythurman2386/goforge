package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
)

type fakeDomainValidator struct {
	routable map[string]bool
	err      error
	calls    []string
}

func (f *fakeDomainValidator) IsDomainRoutable(_ context.Context, domain string) (bool, error) {
	f.calls = append(f.calls, domain)
	if f.err != nil {
		return false, f.err
	}
	return f.routable[domain], nil
}

type domainHandlerFixture struct {
	db       *database.DB
	svc      *project.Service
	owner    *models.User
	other    *models.User
	project  *models.Project
	env      *models.Environment
	otherEnv *models.Environment
}

func newDomainHandlerFixture(t *testing.T) *domainHandlerFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("skipping; no database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if migrateErr := database.MigrateUp(db, cfg); migrateErr != nil {
		t.Skipf("skipping; migrate: %v", migrateErr)
	}

	ctx := context.Background()
	users := repositories.NewUserRepository(db.DB, nil)
	owner := &models.User{Email: "domain-owner-" + uuid.NewString() + "@example.test"}
	other := &models.User{Email: "domain-other-" + uuid.NewString() + "@example.test"}
	for _, user := range []*models.User{owner, other} {
		if createErr := users.Create(ctx, user); createErr != nil {
			t.Fatal(createErr)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM projects WHERE user_id = ANY($1)`, []string{owner.ID, other.ID})
		_, _ = db.Exec(`DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, other.ID})
	})

	projects := repositories.NewProjectRepository(db.DB)
	envs := repositories.NewEnvironmentRepository(db.DB)
	deps := repositories.NewDeploymentRepository(db.DB)
	ctrs := repositories.NewContainerRepository(db.DB)
	svc := project.NewService(projects, envs, repositories.NewEnvVariableRepository(db.DB, nil), deps, ctrs, nil, nil, nil)
	p, err := svc.CreateProject(ctx, &models.Project{
		UserID: owner.ID, Name: "domain-" + uuid.NewString(), RepositoryURL: "https://example.test/repo.git",
		DefaultBranch: "main", DockerfilePath: "Dockerfile", BuildContext: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	environments, err := svc.ListEnvironments(ctx, p.ID)
	if err != nil || len(environments) != 1 {
		t.Fatalf("default environment = %v, %v", environments, err)
	}
	env := environments[0]
	// Seed original values without going through the new DNS-checked mutation.
	// Every refusal in this fixture must retain these exact values.
	if _, updateErr := db.ExecContext(ctx,
		`with project_seed as (
			update projects set custom_domain = $1 where id = $2 returning id
		) update environments set domain = $3 where id = $4`,
		"saved.example.test", p.ID, "env-saved.example.test", env.ID); updateErr != nil {
		t.Fatal(updateErr)
	}
	otherProject, err := svc.CreateProject(ctx, &models.Project{UserID: other.ID, Name: "other-" + uuid.NewString(), RepositoryURL: "https://example.test/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	otherEnvs, err := svc.ListEnvironments(ctx, otherProject.ID)
	if err != nil || len(otherEnvs) != 1 {
		t.Fatalf("other environment = %v, %v", otherEnvs, err)
	}
	return &domainHandlerFixture{db: db, svc: svc, owner: owner, other: other, project: p, env: env, otherEnv: otherEnvs[0]}
}

func domainRequest(target string, form url.Values, user *models.User, params map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, auth.UserContextKey, user)
	return req.WithContext(ctx)
}

func (f *domainHandlerFixture) projectHandler(validator DomainValidator) *ProjectHandler {
	return NewProjectHandler(f.svc, nil, repositories.NewDeploymentRepository(f.db.DB), repositories.NewContainerRepository(f.db.DB), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).WithDomainValidator(validator)
}

func (f *domainHandlerFixture) envHandler(validator DomainValidator) *EnvironmentHandler {
	return NewEnvironmentHandler(f.svc, slog.New(slog.NewTextHandler(io.Discard, nil))).WithDomainValidator(validator)
}

func TestProjectHandler_UpdateSettings_ValidatesBothBeforeWriting(t *testing.T) {
	f := newDomainHandlerFixture(t)
	validator := &fakeDomainValidator{routable: map[string]bool{"app.example.test": true, "env.example.test": true}}
	h := f.projectHandler(validator).WithAudit(&fakeUIAudit{})
	form := func(projectDomain, envDomain string) url.Values {
		return url.Values{"env_id": {f.env.ID}, "custom_domain": {projectDomain}, "domain": {envDomain}}
	}

	for _, tc := range []struct {
		name, projectDomain, envDomain, message string
		status                                  int
		resolverErr                             error
	}{
		{"unresolved environment", "app.example.test", "missing.example.test", "does not resolve", http.StatusBadRequest, nil},
		{"invalid project prevents lookups", "not a domain", "missing.example.test", "invalid domain format", http.StatusBadRequest, nil},
		{"resolver timeout", "app.example.test", "env.example.test", "check the DNS resolver and try again", http.StatusServiceUnavailable, &net.DNSError{Err: "timeout", IsTimeout: true}},
	} {
		// A failure of this precondition would make no-partial-write vacuous.
		originalProject, originalErr := f.svc.GetProject(context.Background(), f.project.ID)
		if originalErr != nil || originalProject.CustomDomain.String != "saved.example.test" {
			t.Fatalf("original project domain = %v, %v", originalProject.CustomDomain, originalErr)
		}
		t.Run(tc.name, func(t *testing.T) {
			validator.err = tc.resolverErr
			validator.calls = nil
			rec := httptest.NewRecorder()
			req := domainRequest("/projects/"+f.project.ID+"/settings", form(tc.projectDomain, tc.envDomain), f.owner, map[string]string{"id": f.project.ID})
			req.Header.Set("HX-Request", "true")
			h.UpdateSettings(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
			}
			p, err := f.svc.GetProject(context.Background(), f.project.ID)
			if err != nil {
				t.Fatal(err)
			}
			env, err := f.svc.GetEnvironment(context.Background(), f.env.ID)
			if err != nil {
				t.Fatal(err)
			}
			if p.CustomDomain.String != "saved.example.test" || env.Domain.String != "env-saved.example.test" {
				t.Fatalf("refusal changed domains: project = %q environment = %q", p.CustomDomain.String, env.Domain.String)
			}
			if tc.status == http.StatusBadRequest && tc.name == "invalid project prevents lookups" && len(validator.calls) != 0 {
				t.Fatalf("invalid format made DNS calls %v", validator.calls)
			}
		})
	}

	validator.err = nil
	validator.calls = nil
	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, domainRequest("/projects/"+f.project.ID+"/settings", form(" APP.EXAMPLE.TEST. ", " ENV.EXAMPLE.TEST. "), f.owner, map[string]string{"id": f.project.ID}))
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("accepted settings status = %d body = %q", rec.Code, rec.Body.String())
	}
	p, _ := f.svc.GetProject(context.Background(), f.project.ID)
	env, _ := f.svc.GetEnvironment(context.Background(), f.env.ID)
	if p.CustomDomain.String != "app.example.test" || env.Domain.String != "env.example.test" {
		t.Fatalf("normalized values not saved: project = %q environment = %q", p.CustomDomain.String, env.Domain.String)
	}
	if len(validator.calls) != 2 || validator.calls[0] != "app.example.test" || validator.calls[1] != "env.example.test" {
		t.Fatalf("DNS calls = %v", validator.calls)
	}

	validator.calls = nil
	rec = httptest.NewRecorder()
	h.UpdateSettings(rec, domainRequest("/projects/"+f.project.ID+"/settings", form("", ""), f.owner, map[string]string{"id": f.project.ID}))
	if rec.Code != http.StatusOK || len(validator.calls) != 0 {
		t.Fatalf("blank clear status = %d DNS calls = %v", rec.Code, validator.calls)
	}
	p, _ = f.svc.GetProject(context.Background(), f.project.ID)
	env, _ = f.svc.GetEnvironment(context.Background(), f.env.ID)
	if p.CustomDomain.Valid || env.Domain.Valid {
		t.Fatalf("blank values did not clear: project = %v environment = %v", p.CustomDomain, env.Domain)
	}
}

func TestProjectHandler_UpdateSettings_EnvironmentOwnershipBeforeWrite(t *testing.T) {
	f := newDomainHandlerFixture(t)
	validator := &fakeDomainValidator{routable: map[string]bool{"app.example.test": true}}
	form := url.Values{"env_id": {f.otherEnv.ID}, "custom_domain": {"app.example.test"}, "domain": {""}}
	rec := httptest.NewRecorder()
	f.projectHandler(validator).UpdateSettings(rec, domainRequest("/projects/"+f.project.ID+"/settings", form, f.owner, map[string]string{"id": f.project.ID}))
	if rec.Code != http.StatusForbidden || len(validator.calls) != 1 {
		t.Fatalf("status = %d DNS calls = %v", rec.Code, validator.calls)
	}
	p, _ := f.svc.GetProject(context.Background(), f.project.ID)
	if p.CustomDomain.String != "saved.example.test" {
		t.Fatalf("foreign environment refusal changed project domain %q", p.CustomDomain.String)
	}
}

func TestProjectHandler_UpdateProject_DomainDNS(t *testing.T) {
	f := newDomainHandlerFixture(t)
	validator := &fakeDomainValidator{routable: map[string]bool{"app.example.test": true}}
	h := f.projectHandler(validator).WithAudit(&fakeUIAudit{})

	rec := httptest.NewRecorder()
	req := domainRequest("/projects/"+f.project.ID, url.Values{"custom_domain": {"missing.example.test"}}, f.owner, map[string]string{"id": f.project.ID})
	h.UpdateProject(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "does not resolve") {
		t.Fatalf("unresolved status = %d body = %q", rec.Code, rec.Body.String())
	}
	p, _ := f.svc.GetProject(context.Background(), f.project.ID)
	if p.CustomDomain.String != "saved.example.test" {
		t.Fatalf("unresolved domain saved %q", p.CustomDomain.String)
	}

	rec = httptest.NewRecorder()
	req = domainRequest("/projects/"+f.project.ID, url.Values{"custom_domain": {" APP.EXAMPLE.TEST. "}}, f.owner, map[string]string{"id": f.project.ID})
	h.UpdateProject(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success status = %d body = %q", rec.Code, rec.Body.String())
	}
	p, _ = f.svc.GetProject(context.Background(), f.project.ID)
	if p.CustomDomain.String != "app.example.test" {
		t.Fatalf("normalized domain not saved %q", p.CustomDomain.String)
	}
}

func TestEnvironmentHandler_UpdateEnvironment_DomainDNSAndOwnership(t *testing.T) {
	f := newDomainHandlerFixture(t)
	validator := &fakeDomainValidator{routable: map[string]bool{"env.example.test": true}}
	h := f.envHandler(validator).WithAudit(&fakeUIAudit{})

	rec := httptest.NewRecorder()
	h.UpdateEnvironment(rec, domainRequest("/projects/"+f.project.ID+"/envs/"+f.env.ID, url.Values{"domain": {"missing.example.test"}}, f.owner, map[string]string{"id": f.project.ID, "envId": f.env.ID}))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "does not resolve") {
		t.Fatalf("unresolved status = %d body = %q", rec.Code, rec.Body.String())
	}
	validator.err = &net.DNSError{Err: "server failure", Server: "192.0.2.53"}
	rec = httptest.NewRecorder()
	h.UpdateEnvironment(rec, domainRequest("/projects/"+f.project.ID+"/envs/"+f.env.ID, url.Values{"domain": {"env.example.test"}}, f.owner, map[string]string{"id": f.project.ID, "envId": f.env.ID}))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "check the DNS resolver and try again") {
		t.Fatalf("resolver failure status = %d body = %q", rec.Code, rec.Body.String())
	}
	env, _ := f.svc.GetEnvironment(context.Background(), f.env.ID)
	if env.Domain.String != "env-saved.example.test" {
		t.Fatalf("rejected mutation saved %q", env.Domain.String)
	}
	validator.err = nil
	rec = httptest.NewRecorder()
	h.UpdateEnvironment(rec, domainRequest("/projects/"+f.project.ID+"/envs/"+f.env.ID, url.Values{"domain": {"missing.example.test"}}, f.other, map[string]string{"id": f.project.ID, "envId": f.env.ID}))
	if rec.Code != http.StatusNotFound || len(validator.calls) != 2 {
		t.Fatalf("ownership status = %d DNS calls = %v", rec.Code, validator.calls)
	}

	rec = httptest.NewRecorder()
	h.UpdateEnvironment(rec, domainRequest("/projects/"+f.project.ID+"/envs/"+f.env.ID, url.Values{"domain": {" ENV.EXAMPLE.TEST. "}}, f.owner, map[string]string{"id": f.project.ID, "envId": f.env.ID}))
	if rec.Code != http.StatusOK {
		t.Fatalf("success status = %d body = %q", rec.Code, rec.Body.String())
	}
	env, _ = f.svc.GetEnvironment(context.Background(), f.env.ID)
	if env.Domain.String != "env.example.test" {
		t.Fatalf("normalized domain not saved %q", env.Domain.String)
	}
	validator.calls = nil
	rec = httptest.NewRecorder()
	h.UpdateEnvironment(rec, domainRequest("/projects/"+f.project.ID+"/envs/"+f.env.ID, url.Values{"domain": {""}}, f.owner, map[string]string{"id": f.project.ID, "envId": f.env.ID}))
	if rec.Code != http.StatusOK || len(validator.calls) != 0 {
		t.Fatalf("clear status = %d DNS calls = %v", rec.Code, validator.calls)
	}
	env, _ = f.svc.GetEnvironment(context.Background(), f.env.ID)
	if env.Domain.Valid {
		t.Fatal("blank did not clear environment domain")
	}
}

func TestValidateDomainDNS_FormatBeforeLookup(t *testing.T) {
	validator := &fakeDomainValidator{routable: map[string]bool{"app.example.test": true}, err: errors.New("unused")}
	for _, domain := range []string{"not a domain", "", "   "} {
		got, failure := validateDomainDNS(context.Background(), validator, domain, "domain")
		if domain != "" && strings.TrimSpace(domain) != "" {
			if failure == nil || failure.status != http.StatusBadRequest {
				t.Fatalf("invalid domain %q failure = %v", domain, failure)
			}
		} else if failure != nil || got != "" {
			t.Fatalf("blank domain %q = %q, %v", domain, got, failure)
		}
	}
	if len(validator.calls) != 0 {
		t.Fatalf("format and blank checks looked up DNS: %v", validator.calls)
	}
}
