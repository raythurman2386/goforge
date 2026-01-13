package services

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewCatalog(t *testing.T) {
	t.Parallel()
	dir := "some/path"
	c := NewCatalog(dir)
	if c == nil {
		t.Fatal("NewCatalog returned nil")
	}
	if c.templatesDir != dir {
		t.Errorf("NewCatalog templatesDir = %q, want %q", c.templatesDir, dir)
	}
	if c.templates == nil {
		t.Error("NewCatalog templates map is nil")
	}
}

func TestCatalog_LoadTemplates(t *testing.T) {
	t.Parallel()
	validTemplate := `
name: "postgres"
display_name: "PostgreSQL"
description: "The world's most advanced open source relational database."
default_version: "15"
`
	invalidYAML := `
name: "postgres"
  indentation_error
`

	tests := []struct {
		name          string
		dirPath       string // Relative to temp dir. If empty, uses root temp dir.
		setupFiles    map[string]string
		setupDirs     []string
		createDir     bool // If true, creates the directory before loading
		wantErr       bool
		expectedCount int
	}{
		{
			name:          "Valid template",
			setupFiles:    map[string]string{"postgres.yaml": validTemplate},
			createDir:     true,
			expectedCount: 1,
		},
		{
			name:          "Ignores non-YAML files",
			setupFiles:    map[string]string{"README.md": "# Readme"},
			createDir:     true,
			expectedCount: 0,
		},
		{
			name:          "Ignores directories",
			setupDirs:     []string{"subdir"},
			createDir:     true,
			expectedCount: 0,
		},
		{
			name: "Invalid YAML",
			setupFiles: map[string]string{
				"postgres.yaml": validTemplate,
				"broken.yaml":   invalidYAML,
			},
			createDir: true,
			wantErr:   true,
		},
		{
			name:          "Directory does not exist",
			dirPath:       "nonexistent",
			createDir:     false,
			expectedCount: 0,
			// Should create directory and return no error
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			baseDir := t.TempDir()
			targetDir := baseDir
			if tt.dirPath != "" {
				targetDir = filepath.Join(baseDir, tt.dirPath)
			}

			if tt.createDir {
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					t.Fatalf("Failed to create test dir: %v", err)
				}

				for filename, content := range tt.setupFiles {
					path := filepath.Join(targetDir, filename)
					if err := os.WriteFile(path, []byte(content), 0644); err != nil {
						t.Fatalf("Failed to write file %s: %v", filename, err)
					}
				}
				for _, dirname := range tt.setupDirs {
					path := filepath.Join(targetDir, dirname)
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatalf("Failed to create dir %s: %v", dirname, err)
					}
				}
			}

			c := NewCatalog(targetDir)
			err := c.LoadTemplates()

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadTemplates() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if len(c.templates) != tt.expectedCount {
					t.Errorf("LoadTemplates() loaded %d templates, want %d", len(c.templates), tt.expectedCount)
				}

				// For the "Directory does not exist" case, verify it was created
				if !tt.createDir {
					if _, err := os.Stat(targetDir); os.IsNotExist(err) {
						t.Error("LoadTemplates() did not create the missing directory")
					}
				}
			}
		})
	}
}

func TestCatalog_GetTemplate(t *testing.T) {
	t.Parallel()
	// No need to create a real directory since we manipulate the map directly
	c := NewCatalog("dummy")
	tmpl := &ServiceTemplate{Name: "redis"}
	c.templates["redis"] = tmpl

	tests := []struct {
		name    string
		reqName string
		want    *ServiceTemplate
		wantErr bool
	}{
		{
			name:    "Existing template",
			reqName: "redis",
			want:    tmpl,
			wantErr: false,
		},
		{
			name:    "Non-existing template",
			reqName: "postgres",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := c.GetTemplate(tt.reqName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCatalog_ListTemplates(t *testing.T) {
	t.Parallel()
	c := NewCatalog("dummy")
	c.templates["redis"] = &ServiceTemplate{Name: "redis"}
	c.templates["postgres"] = &ServiceTemplate{Name: "postgres"}

	list := c.ListTemplates()
	if len(list) != 2 {
		t.Errorf("ListTemplates() returned %d items, want 2", len(list))
	}

	// Verify we got both
	names := make([]string, 0, 2)
	for _, tmpl := range list {
		names = append(names, tmpl.Name)
	}
	sort.Strings(names)

	if names[0] != "postgres" || names[1] != "redis" {
		t.Errorf("ListTemplates() returned names %v, want [postgres redis]", names)
	}
}
