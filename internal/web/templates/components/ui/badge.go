package ui

import "github.com/a-h/templ"

// BadgeTone is the meaning of a Badge.
type BadgeTone string

// Badge tones.
const (
	BadgeNeutral BadgeTone = "neutral"
	BadgePrimary BadgeTone = "primary"
	BadgeSuccess BadgeTone = "success"
	BadgeWarning BadgeTone = "warning"
	BadgeDanger  BadgeTone = "danger"
	BadgeInfo    BadgeTone = "info"
)

// BadgeVariant is the fill of a Badge.
type BadgeVariant string

// Badge variants: soft (tinted with an inset ring, the default), subtle
// (tinted, no ring), solid (filled), outline (ring only).
const (
	BadgeSoft    BadgeVariant = "soft"
	BadgeSubtle  BadgeVariant = "subtle"
	BadgeSolid   BadgeVariant = "solid"
	BadgeOutline BadgeVariant = "outline"
)

// BadgeSize is the text size of a Badge.
type BadgeSize string

// Badge sizes.
const (
	BadgeSM BadgeSize = "sm"
	BadgeMD BadgeSize = "md"
)

// BadgeProps configures Badge. The zero value is a small neutral soft pill.
type BadgeProps struct {
	Tone    BadgeTone
	Variant BadgeVariant
	Size    BadgeSize
	// Square uses rounded-md corners instead of a pill.
	Square bool
	Class  string
	Attrs  templ.Attributes
}

func badgeAttrs(p BadgeProps) templ.Attributes {
	return Attrs(templ.Attributes{"class": Class(
		"inline-flex items-center gap-1.5 font-medium whitespace-nowrap",
		badgeShapeClass(p.Square), badgeSizeClass(p.Size), badgeToneClass(p.Variant, p.Tone),
	)}, templ.Attributes{"class": p.Class}, p.Attrs)
}

func badgeShapeClass(square bool) string {
	if square {
		return "rounded-md"
	}
	return "rounded-full"
}

func badgeSizeClass(s BadgeSize) string {
	if s == BadgeMD {
		return "px-3 py-1 text-sm"
	}
	return "px-2.5 py-0.5 text-xs"
}

// badgeToneClass returns one complete literal per variant and tone so
// Tailwind and scripts/class_check.py see every class.
func badgeToneClass(v BadgeVariant, t BadgeTone) string {
	switch v {
	case BadgeSubtle:
		switch t {
		case BadgePrimary:
			return "bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
		case BadgeSuccess:
			return "bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-300"
		case BadgeWarning:
			return "bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300"
		case BadgeDanger:
			return "bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300"
		case BadgeInfo:
			return "bg-blue-50 text-blue-700 dark:bg-blue-950/40 dark:text-blue-300"
		case BadgeNeutral:
			return "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400"
		}
		// The zero value and unknown values render as BadgeNeutral.
		return badgeToneClass(v, BadgeNeutral)
	case BadgeSolid:
		switch t {
		case BadgePrimary:
			return "bg-primary-700 text-white"
		case BadgeSuccess:
			return "bg-green-700 text-white"
		case BadgeWarning:
			return "bg-amber-700 text-white"
		case BadgeDanger:
			return "bg-red-700 text-white"
		case BadgeInfo:
			return "bg-blue-700 text-white"
		case BadgeNeutral:
			return "bg-gray-900 text-white dark:bg-gray-700"
		}
		// The zero value and unknown values render as BadgeNeutral.
		return badgeToneClass(v, BadgeNeutral)
	case BadgeOutline:
		switch t {
		case BadgePrimary:
			return "ring-1 ring-inset ring-primary-600/40 text-primary-700 dark:text-primary-300"
		case BadgeSuccess:
			return "ring-1 ring-inset ring-green-600/40 text-green-700 dark:text-green-300"
		case BadgeWarning:
			return "ring-1 ring-inset ring-amber-600/40 text-amber-800 dark:text-amber-200"
		case BadgeDanger:
			return "ring-1 ring-inset ring-red-600/40 text-red-700 dark:text-red-300"
		case BadgeInfo:
			return "ring-1 ring-inset ring-blue-600/40 text-blue-700 dark:text-blue-300"
		case BadgeNeutral:
			return "ring-1 ring-inset ring-gray-500/40 text-gray-700 dark:text-gray-300"
		}
		// The zero value and unknown values render as BadgeNeutral.
		return badgeToneClass(v, BadgeNeutral)
	case BadgeSoft:
		switch t {
		case BadgePrimary:
			return "bg-primary-50 text-primary-700 ring-1 ring-inset ring-primary-600/20 dark:bg-primary-900/30 dark:text-primary-300"
		case BadgeSuccess:
			return "bg-green-50 text-green-700 ring-1 ring-inset ring-green-600/20 dark:bg-green-950/40 dark:text-green-300"
		case BadgeWarning:
			return "bg-amber-50 text-amber-800 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-950/40 dark:text-amber-200"
		case BadgeDanger:
			return "bg-red-50 text-red-700 ring-1 ring-inset ring-red-600/20 dark:bg-red-950/40 dark:text-red-300"
		case BadgeInfo:
			return "bg-blue-50 text-blue-700 ring-1 ring-inset ring-blue-600/20 dark:bg-blue-950/40 dark:text-blue-300"
		case BadgeNeutral:
			return "bg-gray-50 text-gray-700 ring-1 ring-inset ring-gray-500/20 dark:bg-gray-800/50 dark:text-gray-300"
		}
		// The zero value and unknown values render as BadgeNeutral.
		return badgeToneClass(v, BadgeNeutral)
	}
	// The zero value and unknown values render as BadgeSoft.
	return badgeToneClass(BadgeSoft, t)
}
