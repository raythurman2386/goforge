package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookHandler_VerifyGitHubSignature(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	payload := []byte(`{"action": "push"}`)

	// Calculate expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		signature string
		payload   []byte
		secret    string
		wantValid bool
	}{
		{
			name:      "valid signature",
			signature: expectedSig,
			payload:   payload,
			secret:    secret,
			wantValid: true,
		},
		{
			name:      "invalid signature",
			signature: "sha256=invalid",
			payload:   payload,
			secret:    secret,
			wantValid: false,
		},
		{
			name:      "missing sha256 prefix",
			signature: hex.EncodeToString(mac.Sum(nil)),
			payload:   payload,
			secret:    secret,
			wantValid: true,
		},
		{
			name:      "empty signature",
			signature: "",
			payload:   payload,
			secret:    secret,
			wantValid: false,
		},
		{
			name:      "wrong secret",
			signature: expectedSig,
			payload:   payload,
			secret:    "wrong-secret",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Handler{}
			valid := h.verifySignature(tt.signature, tt.payload, tt.secret)

			if valid != tt.wantValid {
				t.Errorf("verifySignature() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

func TestWebhookHandler_ParseGitHubPushEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		payload    string
		wantBranch string
		wantCommit string
		wantErr    bool
	}{
		{
			name: "valid push event",
			payload: `{
				"ref": "refs/heads/main",
				"after": "abc123def456",
				"head_commit": {
					"id": "abc123def456",
					"message": "Test commit",
					"author": {"name": "Test User", "email": "test@example.com"}
				},
				"repository": {
					"full_name": "owner/repo",
					"html_url": "https://github.com/owner/repo"
				}
			}`,
			wantBranch: "main",
			wantCommit: "abc123def456",
			wantErr:    false,
		},
		{
			name: "feature branch push",
			payload: `{
				"ref": "refs/heads/feature/new-feature",
				"after": "xyz789",
				"head_commit": {
					"id": "xyz789",
					"message": "Add new feature"
				},
				"repository": {
					"full_name": "owner/repo"
				}
			}`,
			wantBranch: "feature/new-feature",
			wantCommit: "xyz789",
			wantErr:    false,
		},
		{
			name:       "invalid json",
			payload:    `{invalid}`,
			wantBranch: "",
			wantCommit: "",
			wantErr:    true,
		},
		{
			name: "tag push (not a branch)",
			payload: `{
				"ref": "refs/tags/v1.0.0",
				"after": "abc123"
			}`,
			wantBranch: "",
			wantCommit: "abc123",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Handler{}
			event, err := h.parseGitHubPushEvent([]byte(tt.payload))

			if (err != nil) != tt.wantErr {
				t.Errorf("parseGitHubPushEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			if event.Branch != tt.wantBranch {
				t.Errorf("Branch = %q, want %q", event.Branch, tt.wantBranch)
			}
			if event.CommitSHA != tt.wantCommit {
				t.Errorf("CommitSHA = %q, want %q", event.CommitSHA, tt.wantCommit)
			}
		})
	}
}

func TestWebhookHandler_HandleGitHub(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	payload := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "abc123",
		"head_commit": map[string]interface{}{
			"id":      "abc123",
			"message": "Test commit",
		},
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
	}
	body, _ := json.Marshal(payload)

	// Calculate signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name           string
		method         string
		body           []byte
		signature      string
		eventType      string
		wantStatusCode int
	}{
		{
			name:           "valid push event",
			method:         http.MethodPost,
			body:           body,
			signature:      signature,
			eventType:      "push",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "invalid method",
			method:         http.MethodGet,
			body:           body,
			signature:      signature,
			eventType:      "push",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "invalid signature",
			method:         http.MethodPost,
			body:           body,
			signature:      "sha256=invalid",
			eventType:      "push",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "ping event",
			method:         http.MethodPost,
			body:           []byte(`{"zen": "test"}`),
			signature:      "", // ping doesn't need signature
			eventType:      "ping",
			wantStatusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, "/webhooks/github/test-project", bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Hub-Signature-256", tt.signature)
			req.Header.Set("X-GitHub-Event", tt.eventType)

			w := httptest.NewRecorder()

			h := NewHandler(nil, nil, nil, nil, secret, slog.Default())
			h.HandleGitHub(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("HandleGitHub() status = %d, want %d", w.Code, tt.wantStatusCode)
			}
		})
	}
}

func TestPushEvent_IsBranchPush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{"branch push", "refs/heads/main", true},
		{"feature branch", "refs/heads/feature/test", true},
		{"tag push", "refs/tags/v1.0.0", false},
		{"empty ref", "", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := &PushEvent{Ref: tt.ref}
			if got := event.IsBranchPush(); got != tt.want {
				t.Errorf("IsBranchPush() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookHandler_HandleGitea(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	payload := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "abc123",
		"head_commit": map[string]interface{}{
			"id":      "abc123",
			"message": "Test commit",
		},
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
	}
	body, _ := json.Marshal(payload)

	// Calculate signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil)) // Gitea sends hex directly usually

	tests := []struct {
		name           string
		method         string
		body           []byte
		signature      string
		eventType      string
		wantStatusCode int
	}{
		{
			name:           "valid push event",
			method:         http.MethodPost,
			body:           body,
			signature:      signature,
			eventType:      "push",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "invalid method",
			method:         http.MethodGet,
			body:           body,
			signature:      signature,
			eventType:      "push",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "invalid signature",
			method:         http.MethodPost,
			body:           body,
			signature:      "invalid",
			eventType:      "push",
			wantStatusCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, "/webhooks/gitea", bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Gitea-Signature", tt.signature)
			req.Header.Set("X-Gitea-Event", tt.eventType)

			w := httptest.NewRecorder()

			h := NewHandler(nil, nil, nil, nil, secret, slog.Default())
			h.HandleGitea(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("HandleGitea() status = %d, want %d", w.Code, tt.wantStatusCode)
			}
		})
	}
}
