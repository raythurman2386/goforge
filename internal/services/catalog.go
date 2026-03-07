// Package services provides one-click service catalog and instance management.
package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServiceTemplate represents a one-click service template.
type ServiceTemplate struct {
	Name           string           `yaml:"name" json:"name"`
	DisplayName    string           `yaml:"display_name" json:"display_name"`
	Description    string           `yaml:"description" json:"description"`
	Icon           string           `yaml:"icon" json:"icon"`
	Category       string           `yaml:"category" json:"category"`
	Web            bool             `yaml:"web" json:"web"`
	Versions       []string         `yaml:"versions" json:"versions"`
	DefaultVersion string           `yaml:"default_version" json:"default_version"`
	Config         []ConfigField    `yaml:"config" json:"config"`
	Resources      ResourceConfig   `yaml:"resources" json:"resources"`
	Docker         DockerConfig     `yaml:"docker" json:"docker"`
	Connection     ConnectionConfig `yaml:"connection" json:"connection"`
	Backup         BackupConfig     `yaml:"backup" json:"backup"`
}

// ConfigField represents a configuration field for a service.
type ConfigField struct {
	Name     string      `yaml:"name" json:"name"`
	Label    string      `yaml:"label" json:"label"`
	Type     string      `yaml:"type" json:"type"`
	Default  interface{} `yaml:"default,omitempty" json:"default,omitempty"`
	Required bool        `yaml:"required" json:"required"`
	Generate bool        `yaml:"generate,omitempty" json:"generate,omitempty"`
	Length   int         `yaml:"length,omitempty" json:"length,omitempty"`
	Min      int         `yaml:"min,omitempty" json:"min,omitempty"`
	Max      int         `yaml:"max,omitempty" json:"max,omitempty"`
}

// ResourceConfig defines the resource limits for a service.
type ResourceConfig struct {
	CPU    string `yaml:"cpu" json:"cpu"`
	Memory string `yaml:"memory" json:"memory"`
}

// DockerConfig defines the Docker configuration for a service.
type DockerConfig struct {
	Image       string            `yaml:"image" json:"image"`
	Ports       []PortConfig      `yaml:"ports" json:"ports"`
	Environment map[string]string `yaml:"environment" json:"environment"`
	Volumes     []VolumeConfig    `yaml:"volumes" json:"volumes"`
	HealthCheck HealthCheckConfig `yaml:"health_check" json:"health_check"`
}

// PortConfig defines a port mapping.
type PortConfig struct {
	Container int    `yaml:"container" json:"container"`
	Protocol  string `yaml:"protocol" json:"protocol"`
}

// VolumeConfig defines a volume mount.
type VolumeConfig struct {
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
}

// HealthCheckConfig defines the health check for a service container.
type HealthCheckConfig struct {
	Test     []string `yaml:"test" json:"test"`
	Interval string   `yaml:"interval" json:"interval"`
	Timeout  string   `yaml:"timeout" json:"timeout"`
	Retries  int      `yaml:"retries" json:"retries"`
}

// ConnectionConfig defines the connection strings for a service.
type ConnectionConfig struct {
	Internal string `yaml:"internal" json:"internal"`
	External string `yaml:"external" json:"external"`
}

// BackupConfig defines the backup configuration for a service.
type BackupConfig struct {
	Command   string `yaml:"command" json:"command"`
	Extension string `yaml:"extension" json:"extension"`
}

// Catalog manages service templates.
type Catalog struct {
	templatesDir string
	templates    map[string]*ServiceTemplate
}

// NewCatalog creates a new service catalog.
func NewCatalog(templatesDir string) *Catalog {
	return &Catalog{
		templatesDir: templatesDir,
		templates:    make(map[string]*ServiceTemplate),
	}
}

// LoadTemplates loads all service templates from the configured directory.
func (c *Catalog) LoadTemplates() error {
	entries, err := os.ReadDir(c.templatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Create directory if it doesn't exist
			if mkdirErr := os.MkdirAll(c.templatesDir, 0o750); mkdirErr != nil {
				return fmt.Errorf("failed to create templates directory: %w", mkdirErr)
			}
			return nil
		}
		return fmt.Errorf("failed to read templates directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(c.templatesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", entry.Name(), err)
		}

		var tmpl ServiceTemplate
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			return fmt.Errorf("failed to parse template %s: %w", entry.Name(), err)
		}

		c.templates[tmpl.Name] = &tmpl
	}

	return nil
}

// GetTemplate retrieves a template by name.
func (c *Catalog) GetTemplate(name string) (*ServiceTemplate, error) {
	tmpl, ok := c.templates[name]
	if !ok {
		return nil, fmt.Errorf("template not found: %s", name)
	}
	return tmpl, nil
}

// ListTemplates returns all available templates.
func (c *Catalog) ListTemplates() []*ServiceTemplate {
	list := make([]*ServiceTemplate, 0, len(c.templates))
	for _, tmpl := range c.templates {
		list = append(list, tmpl)
	}
	return list
}
