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
				if err := os.MkdirAll(targetDir, 0750); err != nil {
					t.Fatalf("Failed to create test dir: %v", err)
				}

				for filename, content := range tt.setupFiles {
					path := filepath.Join(targetDir, filename)
					if err := os.WriteFile(path, []byte(content), 0600); err != nil {
						t.Fatalf("Failed to write file %s: %v", filename, err)
					}
				}
				for _, dirname := range tt.setupDirs {
					path := filepath.Join(targetDir, dirname)
					if err := os.Mkdir(path, 0750); err != nil {
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

func TestCatalog_TemplateParsing(t *testing.T) {
	t.Parallel()

	fullYAML := `
name: "redis"
display_name: "Redis"
description: "Redis is an open source key-value store."
icon: "redis-icon"
category: "database"
versions:
  - "7.0"
  - "6.2"
default_version: "7.0"
config:
  - name: "password"
    label: "Password"
    type: "password"
    required: true
    generate: true
    length: 32
resources:
  cpu: "1.0"
  memory: "512MB"
docker:
  image: "redis"
  ports:
    - container: 6379
      protocol: "tcp"
  environment:
    REDIS_PASSWORD: "${password}"
  volumes:
    - name: "data"
      path: "/data"
  health_check:
    test: ["CMD", "redis-cli", "ping"]
    interval: "10s"
    timeout: "5s"
    retries: 3
connection:
  internal: "redis://${password}@${host}:6379"
  external: "redis://${password}@${host}:${port}"
backup:
  command: "redis-cli save"
  extension: ".rdb"
`

	tests := []struct {
		name     string
		yaml     string
		validate func(*testing.T, *ServiceTemplate)
		wantErr  bool
	}{
		{
			name: "Full Template",
			yaml: fullYAML,
			validate: func(t *testing.T, tmpl *ServiceTemplate) {
				if tmpl.Name != "redis" {
					t.Errorf("Name = %q, want %q", tmpl.Name, "redis")
				}
				if tmpl.DisplayName != "Redis" {
					t.Errorf("DisplayName = %q, want %q", tmpl.DisplayName, "Redis")
				}
				if len(tmpl.Versions) != 2 {
					t.Errorf("Versions len = %d, want 2", len(tmpl.Versions))
				}
				if tmpl.DefaultVersion != "7.0" {
					t.Errorf("DefaultVersion = %q, want %q", tmpl.DefaultVersion, "7.0")
				}

				// Check Config
				if len(tmpl.Config) != 1 {
					t.Fatalf("Config len = %d, want 1", len(tmpl.Config))
				}
				cfg := tmpl.Config[0]
				if cfg.Name != "password" || !cfg.Generate || cfg.Length != 32 {
					t.Errorf("Config[0] mismatch: %+v", cfg)
				}

				// Check Resources
				if tmpl.Resources.CPU != "1.0" || tmpl.Resources.Memory != "512MB" {
					t.Errorf("Resources mismatch: %+v", tmpl.Resources)
				}

				// Check Docker
				if tmpl.Docker.Image != "redis" {
					t.Errorf("Docker.Image = %q, want %q", tmpl.Docker.Image, "redis")
				}
				if len(tmpl.Docker.Ports) != 1 {
					t.Fatalf("Docker.Ports len = %d, want 1", len(tmpl.Docker.Ports))
				}
				if tmpl.Docker.Ports[0].Container != 6379 {
					t.Errorf("Docker.Ports[0].Container = %d, want 6379", tmpl.Docker.Ports[0].Container)
				}
				if val, ok := tmpl.Docker.Environment["REDIS_PASSWORD"]; !ok || val != "${password}" {
					t.Errorf("Docker.Environment mismatch: %v", tmpl.Docker.Environment)
				}
				if len(tmpl.Docker.Volumes) != 1 {
					t.Errorf("Docker.Volumes len = %d, want 1", len(tmpl.Docker.Volumes))
				}
				hc := tmpl.Docker.HealthCheck
				if hc.Interval != "10s" || hc.Retries != 3 {
					t.Errorf("HealthCheck mismatch: %+v", hc)
				}

				// Check Connection
				if tmpl.Connection.Internal != "redis://${password}@${host}:6379" {
					t.Errorf("Connection.Internal mismatch: %q", tmpl.Connection.Internal)
				}

				// Check Backup
				if tmpl.Backup.Command != "redis-cli save" {
					t.Errorf("Backup.Command mismatch: %q", tmpl.Backup.Command)
				}
			},
			wantErr: false,
		},
		{
			name: "Boundary: Minimal Template",
			yaml: `name: "minimal"`,
			validate: func(t *testing.T, tmpl *ServiceTemplate) {
				if tmpl.Name != "minimal" {
					t.Errorf("Name = %q, want %q", tmpl.Name, "minimal")
				}
				// Verify other fields are zero-valued
				if tmpl.DisplayName != "" {
					t.Errorf("DisplayName should be empty, got %q", tmpl.DisplayName)
				}
				if len(tmpl.Config) != 0 {
					t.Errorf("Config should be empty")
				}
			},
			wantErr: false,
		},
		{
			name:    "Error: Malformed YAML",
			yaml:    "name: \"broken\"\n  : invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temp dir and file
			tempDir := t.TempDir()
			filename := "template.yaml"
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			c := NewCatalog(tempDir)
			err := c.LoadTemplates()

			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadTemplates() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if len(c.templates) != 1 {
					t.Fatalf("Loaded %d templates, want 1", len(c.templates))
				}

				// Get the only template
				var tmpl *ServiceTemplate
				for _, t := range c.templates {
					tmpl = t
					break
				}

				if tt.validate != nil {
					tt.validate(t, tmpl)
				}
			}
		})
	}
}
