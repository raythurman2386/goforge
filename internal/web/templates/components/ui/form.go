package ui

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

// Form controls: Label, Field, Input, Textarea, Select, Checkbox, Switch.
//
// Wiring rules every control follows:
//
//   - The control id comes from Props.ID. When it is empty the id is
//     "field-" + name (plus "-" + value for a checkbox with a value), never
//     the bare name, so the same name in two forms on one page does not
//     collide with an unrelated element id. Pages pass an explicit ID with a
//     form prefix (for example "login-email") when two forms share a page.
//   - Help text renders as <p id="<id>-help"> and the error as
//     <p id="<id>-error">. Both ids are listed in the control's
//     aria-describedby, help first, then error, then DescribedBy.
//   - Error sets aria-invalid="true"; .input[aria-invalid] turns red.
//   - Required renders the native required attribute and, on the label, a
//     visual "*" (aria-hidden) plus "(required)" for screen readers.

// LabelProps configures Label.
type LabelProps struct {
	// For is the id of the labeled control.
	For      string
	Required bool
	// Class is appended to "label".
	Class string
	Attrs templ.Attributes
}

// FieldProps configures Field: a label, the control (children), help, and
// error, with ids that match the control's aria-describedby.
type FieldProps struct {
	// ID is the control's id. Help and error ids derive from it.
	ID       string
	Label    string
	Required bool
	Help     string
	Error    string
	// Class is the wrapper class (spacing, grid span).
	Class string
}

// InputProps configures Input. Type defaults to "text".
type InputProps struct {
	ID           string
	Name         string
	Type         string
	Value        string
	Label        string
	Help         string
	Error        string
	Placeholder  string
	Autocomplete string
	Required     bool
	Disabled     bool
	// DescribedBy adds ids (space separated) of help the page renders itself.
	DescribedBy string
	// Class is the wrapper class; ControlClass is appended to "input".
	Class        string
	ControlClass string
	// Attrs pass through to the <input> (min, max, minlength, spellcheck,
	// hx-*). Do not pass "class"; use ControlClass.
	Attrs templ.Attributes
}

// TextareaProps configures Textarea. Rows defaults to 4.
type TextareaProps struct {
	ID           string
	Name         string
	Value        string
	Label        string
	Help         string
	Error        string
	Placeholder  string
	Rows         int
	Required     bool
	Disabled     bool
	DescribedBy  string
	Class        string
	ControlClass string
	Attrs        templ.Attributes
}

// SelectOption is one <option>. An empty Label shows the Value.
type SelectOption struct {
	Value    string
	Label    string
	Disabled bool
}

// SelectProps configures Select, a native <select>.
type SelectProps struct {
	ID       string
	Name     string
	Label    string
	Help     string
	Error    string
	Options  []SelectOption
	Selected string
	// Blank adds a first option with value "" and BlankLabel as its text.
	// It is selected when Selected is "".
	Blank        bool
	BlankLabel   string
	Required     bool
	Disabled     bool
	DescribedBy  string
	Class        string
	ControlClass string
	Attrs        templ.Attributes
}

// CheckboxProps configures Checkbox, CheckboxControl, and Switch.
type CheckboxProps struct {
	ID   string
	Name string
	// Value is the posted value. Empty omits the attribute, so the browser
	// posts "on".
	Value string
	// Label is the visible label. When empty, the children are the label.
	Label       string
	Description string
	Checked     bool
	Disabled    bool
	Required    bool
	// HiddenFalse renders <input type="hidden" name=Name value="false">
	// after the box, so an unchecked box posts "false". When checked the
	// browser sends Value first; handlers read the first value.
	HiddenFalse bool
	DescribedBy string
	// Class is the wrapper class.
	Class string
	// Attrs pass through to the <input>. Do not pass "class".
	Attrs templ.Attributes
}

// CheckboxGroupProps configures CheckboxGroup, a fieldset of related boxes.
type CheckboxGroupProps struct {
	Legend   string
	Required bool
	// DescribedBy lists ids of help the page renders inside the group.
	DescribedBy string
	Class       string
	Attrs       templ.Attributes
}

// ControlID returns id, or "field-" + name when id is empty.
func ControlID(id, name string) string {
	if id != "" {
		return id
	}
	return "field-" + name
}

// HelpID is the id of a control's help text.
func HelpID(id string) string { return id + "-help" }

// ErrorID is the id of a control's error message.
func ErrorID(id string) string { return id + "-error" }

// DescriptionID is the id of a checkbox or switch description.
func DescriptionID(id string) string { return id + "-description" }

// describedBy joins the help id, error id, and extra ids, skipping empties.
func describedBy(id, help, errMsg, extra string) string {
	var ids []string
	if help != "" {
		ids = append(ids, HelpID(id))
	}
	if errMsg != "" {
		ids = append(ids, ErrorID(id))
	}
	if extra != "" {
		ids = append(ids, strings.Fields(extra)...)
	}
	return strings.Join(ids, " ")
}

func (p InputProps) id() string { return ControlID(p.ID, p.Name) }

func (p InputProps) describedBy() string {
	return describedBy(p.id(), p.Help, p.Error, p.DescribedBy)
}

func (p InputProps) field() FieldProps {
	return FieldProps{ID: p.id(), Label: p.Label, Required: p.Required, Help: p.Help, Error: p.Error, Class: p.Class}
}

func (p TextareaProps) id() string { return ControlID(p.ID, p.Name) }

func (p TextareaProps) describedBy() string {
	return describedBy(p.id(), p.Help, p.Error, p.DescribedBy)
}

func (p TextareaProps) field() FieldProps {
	return FieldProps{ID: p.id(), Label: p.Label, Required: p.Required, Help: p.Help, Error: p.Error, Class: p.Class}
}

func (p TextareaProps) rows() string {
	if p.Rows <= 0 {
		return "4"
	}
	return strconv.Itoa(p.Rows)
}

func (p SelectProps) id() string { return ControlID(p.ID, p.Name) }

func (p SelectProps) describedBy() string {
	return describedBy(p.id(), p.Help, p.Error, p.DescribedBy)
}

func (p SelectProps) field() FieldProps {
	return FieldProps{ID: p.id(), Label: p.Label, Required: p.Required, Help: p.Help, Error: p.Error, Class: p.Class}
}

func (p CheckboxProps) id() string {
	if p.ID != "" || p.Value == "" {
		return ControlID(p.ID, p.Name)
	}
	return ControlID("", p.Name+"-"+p.Value)
}

func (p CheckboxProps) describedBy() string {
	var ids []string
	if p.Description != "" {
		ids = append(ids, DescriptionID(p.id()))
	}
	if p.DescribedBy != "" {
		ids = append(ids, strings.Fields(p.DescribedBy)...)
	}
	return strings.Join(ids, " ")
}

// StringOptions turns plain values into options that show the value.
func StringOptions(values []string) []SelectOption {
	opts := make([]SelectOption, 0, len(values))
	for _, v := range values {
		opts = append(opts, SelectOption{Value: v})
	}
	return opts
}

func optionLabel(o SelectOption) string {
	if o.Label != "" {
		return o.Label
	}
	return o.Value
}

func inputType(t string) string {
	if t == "" {
		return "text"
	}
	return t
}

// LabelClass is the label look: .label (input.css) plus caller classes.
// Use it for a <legend>, or a <p> naming read-only content, where a
// <label for> does not fit. Form controls use Label or Field instead.
func LabelClass(extra string) string {
	return Class("label", extra)
}

// CopyAttrs makes a Button a copy button: ui.js copies the text of the
// element with the given id and reads "Copied!" (or "Copy failed") for two
// seconds. The text stays in the page, never in an attribute.
func CopyAttrs(id string) templ.Attributes {
	return templ.Attributes{"data-ui-copy": id}
}

// inputClass is the text-field look for Input and Textarea.
func inputClass(extra string) string {
	return Class("input", extra)
}

// selectClass is the native select look.
func selectClass(extra string) string {
	return Class("select", extra)
}

// checkboxClass is the native checkbox look (accent color, focus ring).
func checkboxClass() string {
	return "checkbox"
}

// switchClass is the switch track; the thumb is its ::before (input.css).
func switchClass() string {
	return "switch"
}
