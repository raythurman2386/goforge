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
		return "border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200"
	case AlertWarning:
		return "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"
	case AlertSuccess:
		return "border-primary-200 bg-primary-50 text-primary-800 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-200"
	case AlertInfo:
		return "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-200"
	}
	// The zero value and unknown values render as AlertInfo.
	return alertToneClass(AlertInfo)
}

func alertIconClass(t AlertTone) string {
	switch t {
	case AlertError:
		return "text-red-400"
	case AlertWarning:
		return "text-amber-400"
	case AlertSuccess:
		return "text-primary-500"
	case AlertInfo:
		return "text-blue-400"
	}
	// The zero value and unknown values render as AlertInfo.
	return alertIconClass(AlertInfo)
}
