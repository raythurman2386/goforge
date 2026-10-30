package ui

import "github.com/a-h/templ"

// CardProps configures Card.
type CardProps struct {
	// Flush drops the card padding, for a card made of CardHeader,
	// CardContent, a list, or a table edge to edge.
	Flush bool
	// Href renders the whole card as a link.
	Href templ.SafeURL
	// Class is appended (margins, grid placement, hover).
	Class string
	Attrs templ.Attributes
}

func cardAttrs(p CardProps) templ.Attributes {
	return Attrs(templ.Attributes{"class": cardClass(p.Flush)}, templ.Attributes{"class": p.Class}, p.Attrs)
}

// cardClass uses the .card component class (input.css, components layer),
// so caller utilities such as p-8 or border-red-100 override it.
func cardClass(flush bool) string {
	if flush {
		return "card p-0"
	}
	return "card"
}

// CardTitleLevel is the heading level of a CardTitle (2, 3, or 4).
// Pick the level that fits the page outline; the look does not change.
type CardTitleLevel int
