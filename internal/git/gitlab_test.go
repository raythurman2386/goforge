package git

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestGitLabProvider_GetFileContent(t *testing.T) {
	expectedContent := "hello world"
	encodedContent := base64.StdEncoding.EncodeToString([]byte(expectedContent))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expecting path to be something like: /projects/owner%2Frepo/repository/files/path%2Fto%2Ffile.txt
		if r.Method != "GET" {
			t.Errorf("expected GET request, got %s", r.Method)
		}

		if r.URL.Query().Get("ref") != "main" {
			t.Errorf("expected ref=main, got %s", r.URL.Query().Get("ref"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"file_name": "file.txt",
			"file_path": "path/to/file.txt",
			"encoding":  "base64",
			"content":   encodedContent,
		})
	}))
	defer server.Close()

	provider := NewGitLabProvider()

	source := &models.GitSource{
		APIURL: sql.NullString{String: server.URL, Valid: true},
	}

	ctx := context.Background()
	content, err := provider.GetFileContent(ctx, source, "owner", "repo", "main", "path/to/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}
}
