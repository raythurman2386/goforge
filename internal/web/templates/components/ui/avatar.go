package ui

import (
	"strings"
	"unicode"

	"github.com/a-h/templ"
)

// AvatarSize is the diameter of an Avatar.
type AvatarSize string

// Avatar sizes: 32px, 40px, 48px.
const (
	AvatarSM AvatarSize = "sm"
	AvatarMD AvatarSize = "md"
	AvatarLG AvatarSize = "lg"
)

// AvatarProps configures Avatar. GoForge shows initials only, never a remote
// image.
type AvatarProps struct {
	// Name is an email address or a display name; Initials derives the text.
	Name string
	Size AvatarSize
	// Label makes the avatar an image with this accessible name. Leave it
	// empty when the name is visible next to the avatar: the avatar is then
	// decorative (aria-hidden).
	Label string
	Class string
	Attrs templ.Attributes
}

// Initials returns up to two uppercase letters for name. For an email it
// uses the part before "@" and splits on ".", "_", "-", and "+":
// "ray.thurman@example.com" is "RT", "admin@example.com" is "A". An empty
// or symbol-only name is "?".
func Initials(name string) string {
	name = strings.TrimSpace(name)
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]rune, 0, 2)
	for _, p := range parts {
		r := []rune(p)
		out = append(out, unicode.ToUpper(r[0]))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func avatarAttrs(p AvatarProps) templ.Attributes {
	base := templ.Attributes{"class": Class(
		"inline-flex shrink-0 select-none items-center justify-center rounded-full bg-primary-100 font-semibold text-primary-800 dark:bg-primary-900/50 dark:text-primary-200",
		avatarSizeClass(p.Size),
	)}
	if p.Label != "" {
		base["role"] = "img"
		base["aria-label"] = p.Label
	} else {
		base["aria-hidden"] = "true"
	}
	return Attrs(base, templ.Attributes{"class": p.Class}, p.Attrs)
}

func avatarSizeClass(s AvatarSize) string {
	switch s {
	case AvatarSM:
		return "h-8 w-8 text-xs"
	case AvatarLG:
		return "h-12 w-12 text-base"
	case AvatarMD:
		return "h-10 w-10 text-sm"
	}
	// The zero value and unknown values render as AvatarMD.
	return avatarSizeClass(AvatarMD)
}
