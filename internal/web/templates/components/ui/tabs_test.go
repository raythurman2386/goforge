package ui

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestNavTabsAreLinksWithAriaCurrent(t *testing.T) {
	nav := render(t, NavTabs(NavTabsProps{Label: "Project sections", Class: "mb-8"}), "tabs")
	assertHas(t, nav, `<nav aria-label="Project sections"`, `data-ui-navtabs`, "mb-8", "border-b")
	assertLacks(t, nav, `role="tablist"`, `role="tab"`, `data-ui="tabs"`)

	cur := render(t, NavTab(NavTabProps{Href: "/projects/p1?env=e1", Current: true}), "Overview")
	assertHas(t, cur, `<a aria-current="page"`, `href="/projects/p1?env=e1"`, `aria-current="page"`, "aria-[current=page]:border-primary-600", "ui-focus-ring")
	assertLacks(t, cur, `role=`, `aria-selected`)

	other := render(t, NavTab(NavTabProps{Href: "/projects/p1/deployments", Attrs: templ.Attributes{"data-testid": "x"}}), "Deployments")
	assertHas(t, other, `href="/projects/p1/deployments"`, `data-testid="x"`)
	assertLacks(t, other, `aria-current`)
}

func TestTabsARIAWiring(t *testing.T) {
	root := render(t, Tabs(TabsProps{ID: "logs", Class: "mt-4"}), "x")
	assertHas(t, root, `id="logs"`, `data-ui="tabs"`, "mt-4")

	list := render(t, TabList(TabsProps{ID: "logs", Label: "Log source"}), "x")
	assertHas(t, list, `role="tablist"`, `aria-label="Log source"`, `aria-orientation="horizontal"`)

	sel := render(t, Tab(TabProps{TabsID: "logs", Key: "build", Selected: true}), "Build")
	assertHas(t, sel, `<button`, `type="button"`, `role="tab"`, `id="logs-tab-build"`, `aria-controls="logs-panel-build"`,
		`aria-selected="true"`, `tabindex="0"`, `data-ui-tab="build"`)

	off := render(t, Tab(TabProps{TabsID: "logs", Key: "run"}), "Runtime")
	assertHas(t, off, `aria-selected="false"`, `tabindex="-1"`, `aria-controls="logs-panel-run"`)

	panel := render(t, TabPanel(TabPanelProps{TabsID: "logs", Key: "build", Selected: true}), "body")
	assertHas(t, panel, `role="tabpanel"`, `id="logs-panel-build"`, `aria-labelledby="logs-tab-build"`, `tabindex="0"`)
	assertLacks(t, panel, " hidden")

	hidden := render(t, TabPanel(TabPanelProps{TabsID: "logs", Key: "run"}), "body")
	if !strings.Contains(hidden, " hidden") {
		t.Errorf("unselected panel is not hidden:\n%s", hidden)
	}
	if TabID("a", "b") != "a-tab-b" || TabPanelID("a", "b") != "a-panel-b" {
		t.Error("tab ids changed")
	}
}
