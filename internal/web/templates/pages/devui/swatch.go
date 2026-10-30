// Package devui renders the /dev/ui component gallery, registered only when
// GOFORGE_DEV_MODE is true.
package devui

// swatchClass is the background utility for a semantic color token. Whole
// literals so Tailwind and scripts/class_check.py see each one.
func swatchClass(token string) string {
	switch token {
	case "background":
		return "bg-background"
	case "foreground":
		return "bg-foreground"
	case "card":
		return "bg-card"
	case "card-border":
		return "bg-card-border"
	case "border":
		return "bg-border"
	case "divider":
		return "bg-divider"
	case "muted":
		return "bg-muted"
	case "muted-foreground":
		return "bg-muted-foreground"
	case "label":
		return "bg-label"
	case "field":
		return "bg-field"
	case "field-border":
		return "bg-field-border"
	case "field-placeholder":
		return "bg-field-placeholder"
	case "secondary":
		return "bg-secondary"
	case "secondary-foreground":
		return "bg-secondary-foreground"
	case "secondary-hover":
		return "bg-secondary-hover"
	case "secondary-border":
		return "bg-secondary-border"
	case "ring":
		return "bg-ring"
	case "ring-offset":
		return "bg-ring-offset"
	}
	return ""
}

// primarySwatchClass is the background utility for an emerald step.
func primarySwatchClass(step string) string {
	switch step {
	case "50":
		return "bg-primary-50"
	case "100":
		return "bg-primary-100"
	case "200":
		return "bg-primary-200"
	case "300":
		return "bg-primary-300"
	case "400":
		return "bg-primary-400"
	case "500":
		return "bg-primary-500"
	case "600":
		return "bg-primary-600"
	case "700":
		return "bg-primary-700"
	case "800":
		return "bg-primary-800"
	case "900":
		return "bg-primary-900"
	case "950":
		return "bg-primary-950"
	}
	return ""
}
