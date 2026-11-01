package ui

import "github.com/a-h/templ"

// There are two tab strips, and they are not interchangeable:
//
//   - NavTabs is navigation. Each tab is a link to its own server-rendered
//     URL, and the current one carries aria-current="page". There is no
//     tablist role and no JS: Tab moves between links, Enter follows one.
//     Use it when each tab is a route (project Overview, Deployments, ...).
//   - Tabs is the in-page ARIA tabs pattern: a role="tablist" of buttons,
//     each controlling a role="tabpanel" already in the page. static/js/ui.js
//     runs the roving tabindex and the keys (Left and Right wrap, Home, End)
//     with automatic activation: moving focus selects the tab and shows its
//     panel. Use it only when every panel's content is in the page.

// NavTabsProps configures NavTabs.
type NavTabsProps struct {
	// Label is the <nav> aria-label, for example "Project sections".
	Label string
	// Class is appended to the outer wrapper; Attrs pass through to <nav>.
	Class string
	Attrs templ.Attributes
}

// NavTabProps configures one NavTab link.
type NavTabProps struct {
	Href templ.SafeURL
	// Current marks the tab for the page being shown (aria-current="page").
	Current bool
	Attrs   templ.Attributes
}

func navTabsWrapClass() string {
	return "border-b border-border"
}

func navTabsListClass() string {
	return "-mb-px flex gap-8 overflow-x-auto"
}

// navTabClass styles both states from aria-current, so the look always
// matches what assistive tech hears.
func navTabClass() string {
	return "inline-flex items-center whitespace-nowrap rounded-t-ui border-b-2 border-transparent px-1 py-4 text-sm font-bold text-muted-foreground transition-colors duration-200 hover:border-muted-foreground/40 hover:text-foreground-secondary ui-focus-ring aria-[current=page]:border-primary-600 aria-[current=page]:text-primary-700 dark:aria-[current=page]:text-primary-300"
}

func navTabsAttrs(p NavTabsProps) templ.Attributes {
	return Attrs(templ.Attributes{"aria-label": p.Label, "data-ui-navtabs": true}, p.Attrs)
}

func navTabAttrs(p NavTabProps) templ.Attributes {
	a := templ.Attributes{"href": string(p.Href), "class": navTabClass()}
	if p.Current {
		a["aria-current"] = "page"
	}
	return Attrs(a, p.Attrs)
}

// TabsProps configures Tabs and its TabList. Pass the same ID to Tab and
// TabPanel (TabsID) so their ids and aria references line up.
type TabsProps struct {
	// ID is unique in the page; tab and panel ids derive from it.
	ID string
	// Label is the tablist's aria-label.
	Label string
	Class string
	Attrs templ.Attributes
}

// TabProps configures one Tab button. Exactly one Tab per Tabs is Selected
// in the markup; ui.js selects the first if none is.
type TabProps struct {
	TabsID   string
	Key      string
	Selected bool
	Attrs    templ.Attributes
}

// TabPanelProps configures the panel for the Tab with the same Key.
type TabPanelProps struct {
	TabsID   string
	Key      string
	Selected bool
	Class    string
	Attrs    templ.Attributes
}

// TabID is the id of the tab for key in the Tabs tabsID.
func TabID(tabsID, key string) string { return tabsID + "-tab-" + key }

// TabPanelID is the id of the panel for key in the Tabs tabsID.
func TabPanelID(tabsID, key string) string { return tabsID + "-panel-" + key }

func tabListClass() string {
	return "-mb-px flex gap-6 overflow-x-auto border-b border-border"
}

func tabClass() string {
	return "inline-flex items-center whitespace-nowrap rounded-t-ui border-b-2 border-transparent px-1 py-3 text-sm font-bold text-muted-foreground transition-colors duration-200 hover:border-muted-foreground/40 hover:text-foreground-secondary ui-focus-ring aria-selected:border-primary-600 aria-selected:text-primary-700 dark:aria-selected:text-primary-300"
}

func tabPanelClass() string {
	return "pt-4 rounded-ui ui-focus-ring"
}

func tabsAttrs(p TabsProps) templ.Attributes {
	return Attrs(templ.Attributes{"id": p.ID, "data-ui": "tabs"}, templ.Attributes{"class": p.Class}, p.Attrs)
}

func tabListAttrs(p TabsProps) templ.Attributes {
	return templ.Attributes{
		"role":             "tablist",
		"aria-label":       p.Label,
		"aria-orientation": "horizontal",
		"class":            tabListClass(),
	}
}

func tabAttrs(p TabProps) templ.Attributes {
	a := templ.Attributes{
		"type":          "button",
		"role":          "tab",
		"id":            TabID(p.TabsID, p.Key),
		"aria-controls": TabPanelID(p.TabsID, p.Key),
		"data-ui-tab":   p.Key,
		"class":         tabClass(),
	}
	if p.Selected {
		a["aria-selected"] = "true"
		a["tabindex"] = "0"
	} else {
		a["aria-selected"] = "false"
		a["tabindex"] = "-1"
	}
	return Attrs(a, p.Attrs)
}

func tabPanelAttrs(p TabPanelProps) templ.Attributes {
	a := templ.Attributes{
		"role":             "tabpanel",
		"id":               TabPanelID(p.TabsID, p.Key),
		"aria-labelledby":  TabID(p.TabsID, p.Key),
		"tabindex":         "0",
		"data-ui-tabpanel": p.Key,
		"class":            tabPanelClass(),
	}
	if !p.Selected {
		a["hidden"] = true
	}
	return Attrs(a, templ.Attributes{"class": p.Class}, p.Attrs)
}
