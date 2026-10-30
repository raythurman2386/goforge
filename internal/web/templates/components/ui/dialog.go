package ui

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/a-h/templ"
)

// DialogProps configures Dialog. The zero value is an ephemeral modal: the
// server fragment is appended to <body>, static/js/ui.js opens it with
// showModal() on htmx:load and removes it from the page when it closes.
type DialogProps struct {
	// ID is the <dialog> id. Leave it empty to get a generated, unique id,
	// so two dialogs can stack without colliding. The title and error node
	// ids derive from it (DialogTitleID, DialogErrorID).
	ID string
	// Title is the visible heading and the dialog's accessible name.
	Title string
	// Description, when set, renders under the title and becomes the
	// dialog's aria-describedby.
	Description string
	// NonDismissible keeps a backdrop click from closing the dialog (for
	// example a token that is shown once). Escape and the close button
	// still close it.
	NonDismissible bool
	// Static marks an in-page dialog: ui.js does not open it on load and
	// does not remove it on close. Open it with a [data-ui-dialog-open="id"]
	// button. Server fragments leave this false.
	Static bool
	// Class is appended to the dialog panel classes.
	Class string
	// Attrs pass through to the <dialog> element.
	Attrs templ.Attributes
}

// DialogTitleID is the id of a dialog's heading.
func DialogTitleID(dialogID string) string { return dialogID + "-title" }

// DialogDescriptionID is the id of a dialog's description paragraph.
func DialogDescriptionID(dialogID string) string { return dialogID + "-description" }

// DialogErrorID is the id of a dialog's own error node. ui.js writes the
// text of a failed request from inside the dialog there.
func DialogErrorID(dialogID string) string { return dialogID + "-error" }

// NewDialogID returns a fresh "dialog-" id with 8 random hex characters.
func NewDialogID() string { return newID("dialog") }

// newID returns prefix, a dash, and 8 random hex characters.
func newID(prefix string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; keep a usable id.
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// resolveDialog fills in the generated id once per render, so the heading,
// error node, and aria-* references all agree.
func resolveDialog(p DialogProps) DialogProps {
	if p.ID == "" {
		p.ID = NewDialogID()
	}
	return p
}

func dialogMode(p DialogProps) string {
	if p.Static {
		return "static"
	}
	return "ephemeral"
}

// dialogClass is the panel look. The backdrop, the open and close
// transitions, and reduced motion live in input.css (dialog[data-ui-dialog]).
func dialogClass() string {
	return "w-[calc(100%-2rem)] max-w-lg max-h-[90vh] overflow-y-auto rounded-2xl border border-gray-200 bg-white p-0 text-left text-foreground shadow-2xl dark:border-gray-800 dark:bg-gray-900"
}

// confirmDialogClass is dialogClass at the narrower confirm width.
func confirmDialogClass() string {
	return "w-[calc(100%-2rem)] max-w-md max-h-[90vh] overflow-y-auto rounded-2xl border border-gray-200 bg-white p-0 text-left text-foreground shadow-2xl dark:border-gray-800 dark:bg-gray-900"
}

func dialogAttrs(p DialogProps) templ.Attributes {
	base := templ.Attributes{
		"id":              p.ID,
		"data-ui-dialog":  dialogMode(p),
		"aria-labelledby": DialogTitleID(p.ID),
		"class":           dialogClass(),
	}
	if p.Description != "" {
		base["aria-describedby"] = DialogDescriptionID(p.ID)
	}
	if p.NonDismissible {
		base["data-ui-dialog-nondismissible"] = true
	}
	return Attrs(base, templ.Attributes{"class": p.Class}, p.Attrs)
}

// OpenDialogAttrs are the htmx attributes of a button that fetches a
// Dialog fragment from url and appends it to <body>, outside any polled or
// SSE-swapped region. Merge them into ButtonProps.Attrs:
//
//	Attrs: ui.OpenDialogAttrs("/settings/git/new")
func OpenDialogAttrs(url string) templ.Attributes {
	return templ.Attributes{
		"hx-get":        url,
		"hx-target":     "body",
		"hx-swap":       "beforeend",
		"aria-haspopup": "dialog",
	}
}

// ConfirmDialogID is the id of the one kit confirm dialog that
// layouts.Base renders at the end of <body>.
const ConfirmDialogID = "ui-confirm"

// ConfirmTone is the look of the confirm button. Put it on an hx-confirm
// trigger as data-ui-confirm-tone; the default is primary.
type ConfirmTone string

// Confirm tones.
const (
	ConfirmPrimary ConfirmTone = "primary"
	ConfirmDanger  ConfirmTone = "danger"
)

// ConfirmAttrs asks for confirmation through the kit confirm dialog
// (static/js/ui.js hooks htmx:confirm, and plain form submits) before the
// trigger's request runs. label replaces the "Confirm" button text; pass ""
// for the default.
func ConfirmAttrs(question string, tone ConfirmTone, label string) templ.Attributes {
	a := templ.Attributes{"hx-confirm": question}
	if tone == ConfirmDanger {
		a["data-ui-confirm-tone"] = string(ConfirmDanger)
	}
	if label != "" {
		a["data-ui-confirm-label"] = label
	}
	return a
}
