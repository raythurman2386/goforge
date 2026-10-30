package ui

import "github.com/a-h/templ"

// ButtonVariant picks the color treatment of a Button.
type ButtonVariant string

// Button variants. The ghost tones are for icon buttons in rows and
// toolbars: gray at rest, tinted on hover.
const (
	ButtonPrimary      ButtonVariant = "primary"
	ButtonSecondary    ButtonVariant = "secondary"
	ButtonDanger       ButtonVariant = "danger"
	ButtonWarning      ButtonVariant = "warning"
	ButtonGhost        ButtonVariant = "ghost"
	ButtonGhostPrimary ButtonVariant = "ghost-primary"
	ButtonGhostWarning ButtonVariant = "ghost-warning"
	ButtonGhostDanger  ButtonVariant = "ghost-danger"
	ButtonLink         ButtonVariant = "link"
)

// ButtonSize picks padding. ButtonIcon is a square, icon-only target that is
// 44px below md (touch) and 32px from md up; it needs ButtonProps.Label.
// ButtonInline drops padding, for ButtonLink inside text.
type ButtonSize string

// Button sizes.
const (
	ButtonMD     ButtonSize = "md"
	ButtonSM     ButtonSize = "sm"
	ButtonIcon   ButtonSize = "icon"
	ButtonInline ButtonSize = "inline"
)

// ButtonProps configures Button. The zero value is a secondary, medium
// <button type="button">.
type ButtonProps struct {
	Variant ButtonVariant
	Size    ButtonSize
	// Type is the <button> type: "button" (default), "submit", or "reset".
	// Ignored when Href is set.
	Type string
	// Href renders the button as a link (<a href>). Use it for navigation,
	// never for actions that change state.
	Href templ.SafeURL
	// Label is the accessible name (aria-label). Required for ButtonIcon,
	// which has no visible text.
	Label string
	// Disabled renders <button disabled>, or for a link a non-focusable
	// <span aria-disabled="true"> with the same look.
	Disabled bool
	// Class is appended to the variant classes (spacing, width).
	Class string
	// Attrs pass through (hx-*, data-*, id, title). A "class" key is
	// appended like Class.
	Attrs templ.Attributes
}

// ButtonClass returns the classes for a button look, for the rare element
// that cannot be a Button (for example the current page in pagination).
// The variant classes live in the components layer of input.css, so a
// caller's utility classes always win over them.
func ButtonClass(variant ButtonVariant, size ButtonSize) string {
	return Class(buttonVariantClass(variant), buttonSizeClass(size))
}

func buttonVariantClass(v ButtonVariant) string {
	switch v {
	case ButtonPrimary:
		return "btn-primary"
	case ButtonDanger:
		return "btn-danger"
	case ButtonWarning:
		return "btn-warning"
	case ButtonGhost:
		return "btn-ghost"
	case ButtonGhostPrimary:
		return "btn-ghost-primary"
	case ButtonGhostWarning:
		return "btn-ghost-warning"
	case ButtonGhostDanger:
		return "btn-ghost-danger"
	case ButtonLink:
		return "btn-link"
	case ButtonSecondary:
		return "btn-secondary"
	}
	// The zero value and unknown values render as ButtonSecondary.
	return buttonVariantClass(ButtonSecondary)
}

func buttonSizeClass(s ButtonSize) string {
	switch s {
	case ButtonSM:
		return "btn-sm"
	case ButtonIcon:
		return "btn-icon"
	case ButtonInline:
		return "btn-inline"
	case ButtonMD:
		return ""
	}
	// The zero value and unknown values render as ButtonMD.
	return buttonSizeClass(ButtonMD)
}

func buttonType(t string) string {
	switch t {
	case "submit", "reset":
		return t
	default:
		return "button"
	}
}

func buttonAttrs(p ButtonProps) templ.Attributes {
	base := templ.Attributes{"class": ButtonClass(p.Variant, p.Size)}
	if p.Label != "" {
		base["aria-label"] = p.Label
	}
	return Attrs(base, templ.Attributes{"class": p.Class}, p.Attrs)
}
