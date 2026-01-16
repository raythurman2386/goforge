package docker

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeConfig represents a Docker Compose file.
type ComposeConfig struct {
	Version  string                   `yaml:"version"`
	Services map[string]ServiceConfig `yaml:"services"`
	Networks map[string]interface{}   `yaml:"networks"`
	Volumes  map[string]interface{}   `yaml:"volumes"`
}

// ServiceConfig represents a service in a Docker Compose file.
type ServiceConfig struct {
	Image       string      `yaml:"image"`
	Build       BuildConfig `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	Environment Environment `yaml:"environment"`
	Volumes     []string    `yaml:"volumes"`
	Networks    []string    `yaml:"networks"`
	DependsOn   []string    `yaml:"depends_on"`
}

// BuildConfig represents the build configuration for a service.
type BuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for BuildConfig.
func (b *BuildConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		b.Context = value.Value
		return nil
	}
	// Default unmarshal if it's a mapping
	type plain BuildConfig
	return value.Decode((*plain)(b))
}

// Environment represents environment variables, handling both map and list formats.
type Environment map[string]string

// UnmarshalYAML implements the yaml.Unmarshaler interface for Environment.
func (e *Environment) UnmarshalYAML(value *yaml.Node) error {
	if *e == nil {
		*e = make(Environment)
	}

	if value.Kind == yaml.MappingNode {
		var m map[string]string
		if err := value.Decode(&m); err != nil {
			return err
		}
		for k, v := range m {
			(*e)[k] = v
		}
		return nil
	}

	if value.Kind == yaml.SequenceNode {
		var s []string
		if err := value.Decode(&s); err != nil {
			return err
		}
		for _, item := range s {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) == 2 {
				(*e)[parts[0]] = parts[1]
			} else {
				// Handle case where env var is just "KEY" (passed through)
				(*e)[parts[0]] = ""
			}
		}
		return nil
	}

	return fmt.Errorf("unsupported type for environment: %v", value.Kind)
}

// ParseCompose parses a Docker Compose YAML file.
func ParseCompose(data []byte) (*ComposeConfig, error) {
	var config ComposeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}
	return &config, nil
}
