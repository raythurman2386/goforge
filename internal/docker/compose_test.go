package docker

import (
	"testing"
)

func TestParseCompose(t *testing.T) {
	t.Parallel()

	validSimple := `
version: "3.8"
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
`

	validComplex := `
version: '3'
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - db_data:/var/lib/postgresql/data
    networks:
      - backend

  app:
    build: .
    depends_on:
      - db
    ports:
      - "8080:8080"
    networks:
      - backend
      - frontend

networks:
  backend:
  frontend:

volumes:
  db_data:
`

	validWithBuildObject := `
version: '3'
services:
  app:
    build:
      context: ./app
      dockerfile: Dockerfile.dev
`

	validListEnv := `
services:
  app:
    image: nginx
    environment:
      - FOO=bar
      - BAZ=qux
`

	tests := []struct {
		name      string
		yamlData  string
		wantErr   bool
		checkFunc func(*testing.T, *ComposeConfig)
	}{
		{
			name:     "Valid Simple",
			yamlData: validSimple,
			wantErr:  false,
			checkFunc: func(t *testing.T, c *ComposeConfig) {
				if c.Version != "3.8" {
					t.Errorf("Version = %q, want %q", c.Version, "3.8")
				}
				if len(c.Services) != 1 {
					t.Errorf("Services len = %d, want 1", len(c.Services))
				}
				web, ok := c.Services["web"]
				if !ok {
					t.Fatal("Service 'web' not found")
				}
				if web.Image != "nginx:alpine" {
					t.Errorf("web.Image = %q, want %q", web.Image, "nginx:alpine")
				}
				if len(web.Ports) != 1 || web.Ports[0] != "80:80" {
					t.Errorf("web.Ports = %v, want %v", web.Ports, []string{"80:80"})
				}
			},
		},
		{
			name:     "Valid Complex",
			yamlData: validComplex,
			wantErr:  false,
			checkFunc: func(t *testing.T, c *ComposeConfig) {
				if len(c.Services) != 2 {
					t.Errorf("Services len = %d, want 2", len(c.Services))
				}
				app := c.Services["app"]
				if app.Build.Context != "." {
					t.Errorf("app.Build.Context = %q, want %q", app.Build.Context, ".")
				}
				if app.Environment["POSTGRES_PASSWORD"] != "" {
					// Check db env actually
					db := c.Services["db"]
					if db.Environment["POSTGRES_PASSWORD"] != "secret" {
						t.Errorf("db.Environment[POSTGRES_PASSWORD] = %q, want %q", db.Environment["POSTGRES_PASSWORD"], "secret")
					}
				}
			},
		},
		{
			name:     "Valid Build Object",
			yamlData: validWithBuildObject,
			wantErr:  false,
			checkFunc: func(t *testing.T, c *ComposeConfig) {
				app := c.Services["app"]
				if app.Build.Context != "./app" {
					t.Errorf("app.Build.Context = %q, want %q", app.Build.Context, "./app")
				}
				if app.Build.Dockerfile != "Dockerfile.dev" {
					t.Errorf("app.Build.Dockerfile = %q, want %q", app.Build.Dockerfile, "Dockerfile.dev")
				}
			},
		},
		{
			name:     "Valid List Environment",
			yamlData: validListEnv,
			wantErr:  false,
			checkFunc: func(t *testing.T, c *ComposeConfig) {
				app := c.Services["app"]
				if app.Environment["FOO"] != "bar" {
					t.Errorf("app.Environment[FOO] = %q, want %q", app.Environment["FOO"], "bar")
				}
				if app.Environment["BAZ"] != "qux" {
					t.Errorf("app.Environment[BAZ] = %q, want %q", app.Environment["BAZ"], "qux")
				}
			},
		},
		{
			name:      "Invalid YAML",
			yamlData:  "services:\n  web:\n    image: nginx\n  indentation_error",
			wantErr:   true,
			checkFunc: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ParseCompose([]byte(tt.yamlData))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCompose() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, c)
			}
		})
	}
}
