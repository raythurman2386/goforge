// GoForge UI kit runtime (components/ui). Loaded from layouts.Base after
// app.js. No build step; plain ES2017 like app.js.
//
// PR 1 shipped the delegated listener pattern and the htmx:load init pass.
// PR 2 adds the dismissible Alert. Later PRs register Dialog, Dropdown,
// Popover, Tooltip, Tabs, and Sheet. Rules, from the UI kit
// brief (docs/development/frontend.md, "UI kit (Path A)"):
//
//  1. Delegated listeners only. click, keydown, focusin, pointerover, and
//     toggle are attached to document once. Targets are found by data-ui-*
//     attributes, so nothing binds to an element htmx may replace.
//  2. Markup that needs setup is initialized from htmx:load (fires for the
//     first page and every swapped fragment) and marked data-ui-ready, so a
//     second pass over the same element is a no-op.
//  3. Overlays never live inside a polled or SSE-swapped region. Before htmx
//     snapshots the page for history, every open dialog and popover closes.
//  4. Toasts stay in app.js. Do not duplicate LEVEL_CLASSES here.
//  5. Every class string is a complete literal, so tailwind.config.js content
//     and scripts/class_check.py (both list this file) can see it.
//  6. Motion respects prefers-reduced-motion (CSS in input.css).
(function () {
	"use strict";

	if (window.GoForgeUI) return;

	// name -> { init(el), events: { type: handler(evt, el) } }
	var components = {};
	var READY = "data-ui-ready";

	// register adds a component. init runs once per element that matches
	// [data-ui="name"]; events handlers run for delegated events whose target
	// is inside such an element.
	function register(name, spec) {
		components[name] = spec || {};
		if (document.readyState !== "loading") initTree(document);
	}

	function initTree(root) {
		if (!root || !root.querySelectorAll) return;
		var nodes = root.querySelectorAll("[data-ui]:not([" + READY + "])");
		var list = Array.prototype.slice.call(nodes);
		if (root.matches && root.matches("[data-ui]:not([" + READY + "])")) list.unshift(root);
		list.forEach(function (el) {
			var spec = components[el.getAttribute("data-ui")];
			if (!spec) return;
			el.setAttribute(READY, "");
			if (spec.init) spec.init(el);
		});
	}

	function dispatch(evt) {
		var start = evt.target;
		if (!start || !start.closest) return;
		var el = start.closest("[data-ui]");
		while (el) {
			var spec = components[el.getAttribute("data-ui")];
			var handler = spec && spec.events && spec.events[evt.type];
			if (handler) handler(evt, el);
			el = el.parentElement ? el.parentElement.closest("[data-ui]") : null;
		}
	}

	// toggle (popover, details) does not bubble, so listen in the capture phase.
	["click", "keydown", "focusin", "pointerover"].forEach(function (type) {
		document.addEventListener(type, dispatch);
	});
	document.addEventListener("toggle", dispatch, true);

	document.addEventListener("htmx:load", function (evt) {
		initTree(evt.detail && evt.detail.elt ? evt.detail.elt : evt.target);
	});

	// Close open overlays before htmx stores the page in its history cache,
	// so Back never restores a half-open dialog or menu.
	function closeOverlays() {
		document.querySelectorAll("dialog[open]").forEach(function (d) {
			d.close();
		});
		document.querySelectorAll("[popover]").forEach(function (p) {
			try {
				if (p.matches(":popover-open")) p.hidePopover();
			} catch (e) {
				/* :popover-open unsupported: nothing can be open */
			}
		});
	}
	document.addEventListener("htmx:beforeHistorySave", closeOverlays);

	// Alert (components/ui Alert with Dismissible): the close button
	// ([data-ui-dismiss]) removes the alert from the page.
	register("alert", {
		events: {
			click: function (evt, el) {
				var btn = evt.target.closest("[data-ui-dismiss]");
				if (!btn || !el.contains(btn)) return;
				evt.preventDefault();
				el.remove();
			},
		},
	});

	function init() {
		initTree(document);
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", init);
	} else {
		init();
	}

	window.GoForgeUI = {
		register: register,
		init: initTree,
		closeOverlays: closeOverlays,
	};
})();
