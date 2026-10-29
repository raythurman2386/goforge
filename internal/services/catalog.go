// Package services provides one-click service catalog and instance management.
package services

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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

// ConfigField represents a configuration field for a service. Type is one of
// text (default), password, number, bool, or select (see FieldType*).
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
	// Options lists the allowed values of a select field.
	Options []string `yaml:"options,omitempty" json:"options,omitempty"`
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
//
// Templates are read from templatesDir at startup and re-read whenever the
// set of *.yaml files in that directory changes (see ReloadIfChanged), so
// operators can add or edit templates without rebuilding the GoForge image.
// The in-memory map is swapped atomically under mu.
type Catalog struct {
	templatesDir string
	logger       *slog.Logger

	mu          sync.RWMutex
	templates   map[string]*ServiceTemplate
	fingerprint string

	// reloadMu serializes reloads so concurrent requests do not parse twice.
	reloadMu sync.Mutex
}

// NewCatalog creates a new service catalog.
func NewCatalog(templatesDir string) *Catalog {
	return &Catalog{
		templatesDir: templatesDir,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		templates:    make(map[string]*ServiceTemplate),
	}
}

// SetLogger sets the logger used for load and reload messages.
func (c *Catalog) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// Dir returns the configured templates directory.
func (c *Catalog) Dir() string { return c.templatesDir }

// Count returns the number of loaded templates.
func (c *Catalog) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.templates)
}

// LoadTemplates (re)loads all service templates from the configured directory
// and atomically replaces the in-memory catalog.
//
// Files that cannot be read or parsed are skipped and reported in the returned
// error; every valid template is still loaded. If the directory itself cannot
// be read, the current catalog is kept.
func (c *Catalog) LoadTemplates() error {
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()
	return c.load()
}

// ReloadIfChanged reloads the catalog when the *.yaml files in the templates
// directory were added, removed, or modified (by name, size, or mtime) since
// the last load. It reports whether a reload happened.
func (c *Catalog) ReloadIfChanged() (bool, error) {
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	fp, err := c.dirFingerprint()
	if err != nil {
		return false, err
	}
	c.mu.RLock()
	unchanged := fp == c.fingerprint
	c.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	return true, c.load()
}

func (c *Catalog) load() error {
	fp, err := c.dirFingerprint()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(c.templatesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read templates directory: %w", err)
		}
		// Create directory if it doesn't exist
		if mkdirErr := os.MkdirAll(c.templatesDir, 0o750); mkdirErr != nil {
			return fmt.Errorf("failed to create templates directory: %w", mkdirErr)
		}
		entries = nil
	}

	loaded := make(map[string]*ServiceTemplate, len(entries))
	var errs []error
	for _, entry := range entries {
		if !isTemplateFile(entry) {
			continue
		}

		path := filepath.Join(c.templatesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to read template %s: %w", entry.Name(), err))
			continue
		}

		var tmpl ServiceTemplate
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse template %s: %w", entry.Name(), err))
			continue
		}
		if err := tmpl.validateFields(); err != nil {
			errs = append(errs, fmt.Errorf("invalid template %s: %w", entry.Name(), err))
			continue
		}

		loaded[tmpl.Name] = &tmpl
	}

	c.mu.Lock()
	c.templates = loaded
	c.fingerprint = fp
	c.mu.Unlock()

	loadErr := errors.Join(errs...)
	c.logger.Info("loaded service templates", "count", len(loaded), "dir", c.absDir(), "skipped", len(errs))
	if loadErr != nil {
		c.logger.Warn("some service templates failed to load", "dir", c.absDir(), "error", loadErr)
	}
	return loadErr
}

// dirFingerprint summarizes the template files in the directory. A missing
// directory has an empty fingerprint.
func (c *Catalog) dirFingerprint() (string, error) {
	entries, err := os.ReadDir(c.templatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read templates directory: %w", err)
	}

	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !isTemplateFile(entry) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// File vanished between ReadDir and Info; treat as a change.
			parts = append(parts, entry.Name()+"|gone")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s|%d|%d", entry.Name(), info.Size(), info.ModTime().UnixNano()))
	}
	sort.Strings(parts)
	return "dir|" + strings.Join(parts, "\n"), nil
}

func (c *Catalog) absDir() string {
	if abs, err := filepath.Abs(c.templatesDir); err == nil {
		return abs
	}
	return c.templatesDir
}

func isTemplateFile(entry os.DirEntry) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml")
}

// GetTemplate retrieves a template by name.
func (c *Catalog) GetTemplate(name string) (*ServiceTemplate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tmpl, ok := c.templates[name]
	if !ok {
		return nil, fmt.Errorf("template not found: %s", name)
	}
	return tmpl, nil
}

// ListTemplates returns all available templates.
func (c *Catalog) ListTemplates() []*ServiceTemplate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*ServiceTemplate, 0, len(c.templates))
	for _, tmpl := range c.templates {
		list = append(list, tmpl)
	}
	return list
}
