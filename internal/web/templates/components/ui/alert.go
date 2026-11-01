package ui

import "github.com/a-h/templ"

// AlertTone is the severity of an Alert.
type AlertTone string

// Alert tones. Only AlertError interrupts screen readers (role="alert");
// the others are polite (role="status").
const (
	AlertInfo    AlertTone = "info"
	AlertSuccess AlertTone = "success"
	AlertWarning AlertTone = "warning"
	AlertError   AlertTone = "error"
)

// AlertProps configures Alert.
type AlertProps struct {
	Tone AlertTone
	// Title is an optional bold first line; children are the description.
	Title string
	// Dismissible adds a close button (static/js/ui.js removes the alert).
	Dismissible bool
	Class       string
	Attrs       templ.Attributes
}

func alertAttrs(p AlertProps) templ.Attributes {
	base := templ.Attributes{
		"class": Class("flex gap-3 rounded-ui border p-4 text-sm", alertToneClass(p.Tone)),
		"role":  alertRole(p.Tone),
	}
	if p.Dismissible {
		base["data-ui"] = "alert"
	}
	return Attrs(base, templ.Attributes{"class": p.Class}, p.Attrs)
}

func alertRole(t AlertTone) string {
	if t == AlertError {
		return "alert"
	}
	return "status"
}

func alertToneClass(t AlertTone) string {
	switch t {
	case AlertError:
		return "border-danger/30 bg-danger/10 text-danger"
	case AlertWarning:
		return "border-warning/30 bg-warning/10 text-warning"
	case AlertSuccess:
		return "border-primary-200 bg-primary-50 text-primary-800 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-200"
	case AlertInfo:
		return "border-info/30 bg-info/10 text-info"
	}
	// The zero value and unknown values render as AlertInfo.
	return alertToneClass(AlertInfo)
}

func alertIconClass(t AlertTone) string {
	switch t {
	case AlertError:
		return "text-danger"
	case AlertWarning:
		return "text-warning"
	case AlertSuccess:
		return "text-primary-500"
	case AlertInfo:
		return "text-info"
	}
	// The zero value and unknown values render as AlertInfo.
	return alertIconClass(AlertInfo)
}
