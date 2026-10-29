package services

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Config field types a template can declare. An empty type is treated as text.
const (
	FieldTypeText     = "text"
	FieldTypePassword = "password"
	FieldTypeNumber   = "number"
	FieldTypeBool     = "bool"
	FieldTypeSelect   = "select"
)

// ErrInvalidConfig is wrapped by every error that rejects a user-supplied
// config value, so handlers can answer 400 with the message instead of 500.
var ErrInvalidConfig = errors.New("invalid service configuration")

// validateFields checks the template's config schema: bool and select
// defaults must be valid and select fields need options.
func (t *ServiceTemplate) validateFields() error {
	var errs []error
	for _, f := range t.Config {
		switch f.Type {
		case FieldTypeBool:
			if f.Default != nil {
				if _, err := parseBoolValue(f.Default); err != nil {
					errs = append(errs, fmt.Errorf("config field %s: default %v is not true or false", f.Name, f.Default))
				}
			}
		case FieldTypeSelect:
			if len(f.Options) == 0 {
				errs = append(errs, fmt.Errorf("config field %s: select needs options", f.Name))
				continue
			}
			if f.Default != nil {
				if d := fmt.Sprintf("%v", f.Default); !slices.Contains(f.Options, d) {
					errs = append(errs, fmt.Errorf("config field %s: default %q is not one of %v", f.Name, d, f.Options))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// BoolDefault reports whether a bool field starts checked.
func (f ConfigField) BoolDefault() bool {
	b, err := parseBoolValue(f.Default)
	return err == nil && b
}

// IsTrue reports whether a stored or submitted config value means true.
func IsTrue(v interface{}) bool {
	b, err := parseBoolValue(v)
	return err == nil && b
}

func parseBoolValue(v interface{}) (bool, error) {
	switch b := v.(type) {
	case bool:
		return b, nil
	case nil:
		return false, errors.New("missing value")
	default:
		return strconv.ParseBool(strings.TrimSpace(fmt.Sprintf("%v", v)))
	}
}

// normalizeConfig validates bool and select values in config and rewrites
// them in place: bools become "true" or "false", selects keep their option.
// A missing or blank value takes the field default (bools default to false).
// Other field types are left alone. Errors wrap ErrInvalidConfig.
func normalizeConfig(tmpl *ServiceTemplate, config map[string]interface{}) error {
	var errs []error
	for _, f := range tmpl.Config {
		switch f.Type {
		case FieldTypeBool:
			raw, ok := config[f.Name]
			if !ok || raw == nil || strings.TrimSpace(fmt.Sprintf("%v", raw)) == "" {
				config[f.Name] = strconv.FormatBool(f.BoolDefault())
				continue
			}
			b, err := parseBoolValue(raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s must be true or false, got %q", fieldLabel(f), fmt.Sprintf("%v", raw)))
				continue
			}
			config[f.Name] = strconv.FormatBool(b)
		case FieldTypeSelect:
			val := ""
			if raw, ok := config[f.Name]; ok && raw != nil {
				val = strings.TrimSpace(fmt.Sprintf("%v", raw))
			}
			if val == "" && f.Default != nil {
				val = fmt.Sprintf("%v", f.Default)
			}
			if val == "" {
				if f.Required {
					errs = append(errs, fmt.Errorf("%s is required; choose one of %s", fieldLabel(f), strings.Join(f.Options, ", ")))
					continue
				}
				config[f.Name] = ""
				continue
			}
			if !slices.Contains(f.Options, val) {
				errs = append(errs, fmt.Errorf("%s must be one of %s, got %q", fieldLabel(f), strings.Join(f.Options, ", "), val))
				continue
			}
			config[f.Name] = val
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}

func fieldLabel(f ConfigField) string {
	if f.Label != "" {
		return f.Label
	}
	return f.Name
}
