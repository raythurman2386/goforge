// GoForge UI kit runtime (components/ui). Loaded from layouts.Base after
// app.js. No build step; plain ES2017 like app.js.
//
// PR 1 shipped the delegated listener pattern and the htmx:load init pass.
// PR 2 added the dismissible Alert. PR 4 added Dialog and the hx-confirm
// dialog. PR 5 added Dropdown, Popover, and Tooltip on the popover
// attribute. PR 6 added in-page Tabs and the Sheet (an edge-pinned static
// dialog on the same runtime). PR 7 adds copy buttons (data-ui-copy). The
// full contract is docs/reference/ui-kit.md. Rules, from the UI kit
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
		var root = evt.detail && evt.detail.elt ? evt.detail.elt : evt.target;
		initTree(root);
		initDialogs(root);
		initSheets(root);
	});

	// Close open overlays before htmx stores the page in its history cache,
	// so Back never restores a half-open dialog or menu.
	function closeOverlays() {
		hideTip(false);
		document.querySelectorAll("dialog[open]").forEach(function (d) {
			d.close();
			d._uiModal = false;
		});
		// close events run later; unlock now so htmx saves the page's
		// real scroll offset and unpinned body.
		syncScrollLock();
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

	// --- Dialog and confirm (components/ui Dialog, ConfirmDialog) ----------
	//
	// A server fragment renders <dialog data-ui-dialog="ephemeral">, appended
	// to <body> (ui.OpenDialogAttrs). On htmx:load it opens with showModal(),
	// which gives the focus trap, the inert page, and Escape natively. The
	// element that issued the request (htmx:beforeRequest) is its opener, and
	// focus returns there on close. An ephemeral dialog is removed on close.
	// data-ui-dialog="static" dialogs stay in the page and open from a
	// [data-ui-dialog-open="id"] button.
	var DIALOG = "dialog[data-ui-dialog]";
	var VERBS = ["hx-get", "hx-post", "hx-put", "hx-patch", "hx-delete"];
	var FOCUS_FIRST =
		"[autofocus], input:not([type=hidden]):not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([data-ui-dialog-close]):not([disabled])";
	var lastTrigger = null;
	var pendingConfirm = null;
	var backdropDown = null;

	// Only a request the user started can open a dialog, so only those record
	// the opener. Polls (every Ns), load triggers, and event triggers such as
	// "tokenCreated from:body" (which fires before the swap of the dialog
	// that caused it) must not overwrite it.
	var USER_EVENTS = { click: 1, submit: 1, keydown: 1, keyup: 1, change: 1, input: 1 };

	function isUserRequest(detail) {
		var ev = detail && detail.requestConfig && detail.requestConfig.triggeringEvent;
		return !!ev && USER_EVENTS[ev.type] === 1;
	}

	function isHtmxRequester(el) {
		if (!el || !el.hasAttribute) return false;
		for (var i = 0; i < VERBS.length; i++) if (el.hasAttribute(VERBS[i])) return true;
		return false;
	}

	// A poll or SSE swap may replace the element a dialog or confirm came
	// from. Find its live twin: same id, else the same request verb and path.
	function liveTwin(el) {
		if (!el || el.isConnected) return el;
		if (el.id) {
			var byId = document.getElementById(el.id);
			if (byId) return byId;
		}
		for (var i = 0; i < VERBS.length; i++) {
			var v = el.getAttribute && el.getAttribute(VERBS[i]);
			if (v) {
				var m = document.querySelector("[" + VERBS[i] + '="' + CSS.escape(v) + '"]');
				if (m) return m;
			}
		}
		return null;
	}

	// The opener of a dialog that htmx just swapped in. A trigger inside a
	// dialog that the response replaced (hx-target="closest dialog") hands
	// over that dialog's opener, so focus goes back to the page, not nowhere.
	function takeOpener() {
		var t = lastTrigger;
		lastTrigger = null;
		if (t && !t.isConnected && t.closest) {
			var host = t.closest(DIALOG);
			if (host && host._uiOpener) return host._uiOpener;
		}
		return t || document.activeElement;
	}

	// A menu item that opened a dialog is hidden with its closed menu by the
	// time the dialog closes; focus goes to the menu's trigger instead.
	function visibleOpener(el) {
		var p = el && el.closest && el.closest("[popover]");
		if (!p) return el;
		var open = false;
		try {
			open = p.matches(":popover-open");
		} catch (e) {
			/* no popover support */
		}
		if (open) return el;
		var t = p.id && document.querySelector('[popovertarget="' + CSS.escape(p.id) + '"]');
		return t || el;
	}

	// A Sheet (data-ui-sheet) is mostly navigation: focus its first link or
	// control, not the first non-close button (which may be Log out).
	var SHEET_FOCUS_FIRST = "[autofocus], a[href], " + FOCUS_FIRST;

	function setOpenerExpanded(el, value) {
		if (el && el.hasAttribute && el.hasAttribute("aria-expanded")) el.setAttribute("aria-expanded", value);
	}

	function openDialog(d, opener) {
		if (!d || d.open || !d.isConnected) return;
		d._uiOpener = opener || document.activeElement;
		d.showModal();
		d._uiModal = true;
		syncScrollLock();
		setOpenerExpanded(d._uiOpener, "true");
		var first = d.querySelector(d.hasAttribute("data-ui-sheet") ? SHEET_FOCUS_FIRST : FOCUS_FIRST);
		if (first) first.focus();
	}

	// --- Scroll lock while a modal dialog is open ---------------------------
	//
	// showModal() makes the page inert, but it does not stop it scrolling:
	// a wheel or touch over the backdrop still scrolls the document, and
	// iOS Safari scrolls and bounces the page behind an open Sheet. While
	// any dialog opened here is open, <html> gets overflow: hidden and
	// <body> is pinned at its scroll offset with position: fixed (the only
	// lock iOS honors). The last close restores the inline styles and the
	// offset. Stacked dialogs share one lock: syncScrollLock reads the open
	// dialogs instead of counting, so a dialog removed without a close
	// event cannot leave the page stuck.
	var scrollLock = null;

	function anyModalOpen() {
		return Array.prototype.some.call(document.querySelectorAll(DIALOG + "[open]"), function (d) {
			return d._uiModal;
		});
	}

	function lockScroll() {
		var html = document.documentElement;
		var body = document.body;
		if (!body) return;
		var gap = window.innerWidth - html.clientWidth;
		scrollLock = {
			x: window.scrollX,
			y: window.scrollY,
			html: html.getAttribute("style"),
			body: body.getAttribute("style"),
		};
		html.style.overflow = "hidden";
		html.style.overscrollBehavior = "none";
		body.style.position = "fixed";
		body.style.top = -scrollLock.y + "px";
		body.style.left = -scrollLock.x + "px";
		body.style.right = "0";
		body.style.overflow = "hidden";
		if (gap > 0) body.style.paddingRight = gap + "px";
		html.setAttribute("data-ui-scroll-locked", "");
	}

	function restoreStyle(el, value) {
		if (value === null) el.removeAttribute("style");
		else el.setAttribute("style", value);
	}

	function unlockScroll() {
		var lock = scrollLock;
		scrollLock = null;
		var html = document.documentElement;
		restoreStyle(html, lock.html);
		if (document.body) restoreStyle(document.body, lock.body);
		html.removeAttribute("data-ui-scroll-locked");
		window.scrollTo({ left: lock.x, top: lock.y, behavior: "instant" });
	}

	function syncScrollLock() {
		var open = anyModalOpen();
		if (open && !scrollLock) lockScroll();
		else if (!open && scrollLock) unlockScroll();
	}

	// A swap can drop an open dialog without a close event, and a page
	// restored from the back/forward cache comes back as it was left.
	document.addEventListener("htmx:afterSettle", syncScrollLock);
	window.addEventListener("pageshow", syncScrollLock);

	function closeDialog(d, value) {
		if (d && d.open) d.close(value);
	}

	function errorNode(d) {
		return d.querySelector("[data-ui-dialog-error]");
	}

	function initDialogs(root) {
		if (!root || !root.querySelectorAll) return;
		var sel = DIALOG + '[data-ui-dialog="ephemeral"]:not([' + READY + "])";
		var list = Array.prototype.slice.call(root.querySelectorAll(sel));
		if (root.matches && root.matches(sel)) list.unshift(root);
		list.forEach(function (d) {
			d.setAttribute(READY, "");
			openDialog(d, takeOpener());
		});
	}

	// close does not bubble; listen in the capture phase.
	document.addEventListener(
		"close",
		function (evt) {
			var d = evt.target;
			if (!d || !d.matches || !d.matches(DIALOG)) return;
			// Reopened before this queued event ran (a new confirm replaced
			// a stale one): nothing to tear down.
			if (d.open) return;
			// Unlock before focus goes back, so the page is at its old
			// offset when the opener takes focus.
			d._uiModal = false;
			syncScrollLock();
			setOpenerExpanded(liveTwin(d._uiOpener), "false");
			var back = visibleOpener(liveTwin(d._uiOpener));
			d._uiOpener = null;
			var confirmed = null;
			if (d.hasAttribute("data-ui-confirm")) {
				confirmed = d.returnValue === "confirm" ? pendingConfirm : null;
				pendingConfirm = null;
			}
			if (d.getAttribute("data-ui-dialog") === "ephemeral") d.remove();
			if (back && back !== document.body && back.focus) back.focus();
			if (confirmed) confirmed.run();
		},
		true,
	);

	// A backdrop click lands on the <dialog> itself, outside its box. Both
	// the press and the release must be there, so a text selection dragged
	// out of an input does not close the dialog.
	function onBackdrop(evt) {
		var d = evt.target;
		if (!d || !d.matches || !d.matches(DIALOG) || !d.open) return null;
		var r = d.getBoundingClientRect();
		var outside = evt.clientX < r.left || evt.clientX > r.right || evt.clientY < r.top || evt.clientY > r.bottom;
		return outside ? d : null;
	}

	document.addEventListener("pointerdown", function (evt) {
		backdropDown = onBackdrop(evt);
	});

	document.addEventListener("click", function (evt) {
		var t = evt.target;
		if (!t || !t.closest) return;
		var closer = t.closest("[data-ui-dialog-close]");
		if (closer) {
			evt.preventDefault();
			closeDialog(closer.closest(DIALOG), "cancel");
			return;
		}
		var accept = t.closest("[data-ui-confirm-accept]");
		if (accept) {
			evt.preventDefault();
			closeDialog(accept.closest(DIALOG), "confirm");
			return;
		}
		var cancel = t.closest("[data-ui-confirm-cancel]");
		if (cancel) {
			evt.preventDefault();
			closeDialog(cancel.closest(DIALOG), "cancel");
			return;
		}
		var opener = t.closest("[data-ui-dialog-open]");
		if (opener) {
			var target = document.getElementById(opener.getAttribute("data-ui-dialog-open"));
			if (target && target.matches(DIALOG)) {
				evt.preventDefault();
				openDialog(target, opener);
			}
			return;
		}
		var d = onBackdrop(evt);
		if (d && d === backdropDown && !d.hasAttribute("data-ui-dialog-nondismissible")) closeDialog(d, "cancel");
		backdropDown = null;
	});

	// Confirm. askConfirm opens the one ConfirmDialog (layouts.Base) and calls
	// run only when Confirm is pressed. Cancel, Escape, and the backdrop send
	// nothing.
	function askConfirm(source, question, run) {
		var d = document.getElementById("ui-confirm");
		if (!d) {
			// A page without layouts.Base: keep the request gated.
			if (window.confirm(question)) run();
			return;
		}
		closeDialog(d, "cancel");
		var message = d.querySelector("[data-ui-confirm-message]");
		if (message) message.textContent = question;
		var accept = d.querySelector("[data-ui-confirm-accept]");
		if (accept) {
			accept.textContent = source.getAttribute("data-ui-confirm-label") || "Confirm";
			if (source.getAttribute("data-ui-confirm-tone") === "danger") {
				accept.className = "btn-danger";
			} else {
				accept.className = "btn-primary";
			}
		}
		d.returnValue = "";
		pendingConfirm = { run: run };
		openDialog(d, source);
	}

	// htmx 2.0.4 fires htmx:confirm before every request; detail.question is
	// the hx-confirm text and detail.issueRequest(true) sends the request
	// without htmx's own window.confirm().
	document.addEventListener("htmx:confirm", function (evt) {
		var detail = evt.detail || {};
		if (!detail.question) return;
		var elt = detail.elt;
		evt.preventDefault();
		if (elt._uiConfirmed) {
			elt._uiConfirmed = false;
			detail.issueRequest(true);
			return;
		}
		var source = (elt.closest && elt.closest("[hx-confirm]")) || elt;
		askConfirm(source, detail.question, function () {
			if (elt.isConnected) {
				detail.issueRequest(true);
				return;
			}
			// A poll re-rendered the trigger while the prompt was open; htmx
			// drops requests from detached elements, so ask its twin.
			var twin = liveTwin(elt);
			if (!twin) return;
			twin._uiConfirmed = true;
			if (twin.tagName === "FORM") twin.requestSubmit();
			else twin.click();
		});
	});

	// hx-confirm on a plain (non-htmx) form or submit button: the same
	// dialog gates the native submit.
	document.addEventListener("submit", function (evt) {
		if (evt.defaultPrevented) return;
		var form = evt.target;
		var btn = evt.submitter || null;
		var source = btn && btn.hasAttribute("hx-confirm") ? btn : form.hasAttribute("hx-confirm") ? form : null;
		if (!source || isHtmxRequester(form) || isHtmxRequester(btn)) return;
		if (form._uiConfirmed) {
			form._uiConfirmed = false;
			return;
		}
		evt.preventDefault();
		askConfirm(source, source.getAttribute("hx-confirm"), function () {
			form._uiConfirmed = true;
			if (btn) form.requestSubmit(btn);
			else form.requestSubmit();
		});
	});

	document.addEventListener("htmx:beforeRequest", function (evt) {
		var elt = evt.detail && evt.detail.elt;
		if (!elt) return;
		if (isUserRequest(evt.detail)) lastTrigger = elt;
		var d = elt.closest && elt.closest(DIALOG);
		var err = d && errorNode(d);
		if (err) {
			err.textContent = "";
			err.classList.add("hidden");
		}
	});

	// A request from inside a dialog closes it on success and shows its error
	// text in the dialog's own error node on failure. Triggers that open
	// another dialog (aria-haspopup="dialog") keep their dialog open, so
	// dialogs can stack.
	document.addEventListener("htmx:afterRequest", function (evt) {
		var elt = evt.detail && evt.detail.elt;
		var d = elt && elt.closest && elt.closest(DIALOG);
		// Disconnected: the response replaced this dialog (closest dialog).
		if (!d || !d.isConnected || d.hasAttribute("data-ui-confirm")) return;
		if (evt.detail.successful) {
			if (elt.getAttribute("aria-haspopup") !== "dialog") closeDialog(d, "done");
			return;
		}
		var err = errorNode(d);
		var GF = window.GoForge;
		if (err) {
			err.textContent = GF && GF.errorMessage ? GF.errorMessage(evt.detail.xhr) : "Request failed.";
			err.classList.remove("hidden");
		}
	});

	// --- Sheet (components/ui Sheet) ---------------------------------------
	//
	// A Sheet is a data-ui-dialog="static" dialog, so everything above
	// applies: [data-ui-dialog-open] opens it with showModal(), Escape and
	// the backdrop close it, and focus returns to the opener. The slide is
	// CSS. data-ui-sheet-close-at="<media query>" closes an open sheet when
	// the query starts to match (the mobile nav sheet once the desktop
	// sidebar shows at md).
	var sheetQueries = {};

	function watchSheet(d) {
		var q = d.getAttribute("data-ui-sheet-close-at");
		if (!q || sheetQueries[q] || !window.matchMedia) return;
		var mql = window.matchMedia(q);
		sheetQueries[q] = mql;
		var onChange = function () {
			if (!mql.matches) return;
			document.querySelectorAll('dialog[data-ui-sheet][open][data-ui-sheet-close-at="' + CSS.escape(q) + '"]').forEach(function (s) {
				closeDialog(s, "cancel");
			});
		};
		if (mql.addEventListener) mql.addEventListener("change", onChange);
		else if (mql.addListener) mql.addListener(onChange);
	}

	function initSheets(root) {
		if (!root || !root.querySelectorAll) return;
		var sel = "dialog[data-ui-sheet][data-ui-sheet-close-at]";
		var list = Array.prototype.slice.call(root.querySelectorAll(sel));
		if (root.matches && root.matches(sel)) list.unshift(root);
		list.forEach(watchSheet);
	}

	// --- Tabs (components/ui Tabs, TabList, Tab, TabPanel) ------------------
	//
	// WAI-ARIA tabs with automatic activation. Only the selected tab is in
	// the Tab order (roving tabindex). Left and Right move to the previous
	// and next tab and wrap, Home and End jump to the first and last, and the
	// tab that gets focus is selected and shows its panel. Nested Tabs only
	// answer for their own tabs. NavTabs (links) need none of this.
	function ownTabs(root) {
		return Array.prototype.filter.call(root.querySelectorAll('[role="tab"][data-ui-tab]'), function (t) {
			return t.closest('[data-ui="tabs"]') === root;
		});
	}

	function selectTab(root, tab, focus) {
		ownTabs(root).forEach(function (t) {
			var on = t === tab;
			t.setAttribute("aria-selected", on ? "true" : "false");
			t.setAttribute("tabindex", on ? "0" : "-1");
			var panel = document.getElementById(t.getAttribute("aria-controls"));
			if (panel) panel.hidden = !on;
		});
		if (focus) tab.focus();
	}

	register("tabs", {
		init: function (el) {
			var tabs = ownTabs(el);
			if (!tabs.length) return;
			var selected = tabs.filter(function (t) {
				return t.getAttribute("aria-selected") === "true";
			})[0];
			selectTab(el, selected || tabs[0], false);
		},
		events: {
			click: function (evt, el) {
				var tab = evt.target.closest('[role="tab"][data-ui-tab]');
				if (!tab || tab.closest('[data-ui="tabs"]') !== el) return;
				selectTab(el, tab, true);
			},
			keydown: function (evt, el) {
				var tab = evt.target.closest('[role="tab"][data-ui-tab]');
				if (!tab || tab.closest('[data-ui="tabs"]') !== el) return;
				var tabs = ownTabs(el);
				var i = tabs.indexOf(tab);
				var next = -1;
				switch (evt.key) {
					case "ArrowRight":
						next = (i + 1) % tabs.length;
						break;
					case "ArrowLeft":
						next = (i - 1 + tabs.length) % tabs.length;
						break;
					case "Home":
						next = 0;
						break;
					case "End":
						next = tabs.length - 1;
						break;
					default:
						return;
				}
				evt.preventDefault();
				selectTab(el, tabs[next], true);
			},
		},
	});

	// --- Overlays: Dropdown, Popover, Tooltip (components/ui overlay.go) ---
	//
	// Each panel carries the popover attribute, so the browser renders it in
	// the top layer and light-dismisses popover="auto" (menu, popover) on an
	// outside click or Escape. A trigger with popovertarget opens it without
	// JS. This runtime adds placement under or over the trigger with a
	// viewport flip, the menu key model, focus moves and focus return, and
	// aria-expanded sync on toggle. Tooltips (popover="manual") show on hover
	// and keyboard focus after a delay, stay open while hovered (WCAG
	// 1.4.13), and hide on Escape without moving focus.
	var OVERLAY = "[data-ui-overlay]";
	var GAP = 6;
	var EDGE = 8;
	var TIP_SHOW_MS = 300;
	var TIP_HIDE_MS = 150;

	function isOpen(p) {
		try {
			return p.matches(":popover-open");
		} catch (e) {
			return false;
		}
	}

	function invokers(p) {
		if (!p.id) return [];
		return Array.prototype.slice.call(document.querySelectorAll('[popovertarget="' + CSS.escape(p.id) + '"]:not([popovertargetaction="hide"])'));
	}

	// The element the overlay is placed against and focus returns to.
	function anchorOf(p) {
		if (p._uiAnchor && p._uiAnchor.isConnected) return p._uiAnchor;
		var list = invokers(p);
		return list.length ? list[0] : null;
	}

	// place positions an open overlay next to its anchor: the preferred side
	// (data-ui-side) unless the other side has more room, aligned by
	// data-ui-align, clamped inside the viewport.
	function place(p) {
		var a = anchorOf(p);
		if (!a || !isOpen(p)) return;
		var r = a.getBoundingClientRect();
		var w = p.offsetWidth;
		var h = p.offsetHeight;
		var vw = document.documentElement.clientWidth;
		var vh = document.documentElement.clientHeight;
		var below = vh - r.bottom - GAP - EDGE;
		var above = r.top - GAP - EDGE;
		var side = p.getAttribute("data-ui-side") || "bottom";
		if (side === "top" && above < h && below > above) side = "bottom";
		else if (side === "bottom" && below < h && above > below) side = "top";
		var top = side === "top" ? r.top - GAP - h : r.bottom + GAP;
		var align = p.getAttribute("data-ui-align") || "start";
		var left = align === "end" ? r.right - w : align === "center" ? r.left + (r.width - w) / 2 : r.left;
		left = Math.max(EDGE, Math.min(left, vw - w - EDGE));
		top = Math.max(EDGE, Math.min(top, vh - h - EDGE));
		p.style.top = Math.round(top) + "px";
		p.style.left = Math.round(left) + "px";
		p.setAttribute("data-ui-placed", side);
	}

	function placeOpen() {
		document.querySelectorAll(OVERLAY).forEach(function (p) {
			if (isOpen(p)) place(p);
		});
	}
	var placeQueued = false;
	function queuePlace() {
		if (placeQueued) return;
		placeQueued = true;
		requestAnimationFrame(function () {
			placeQueued = false;
			placeOpen();
		});
	}
	window.addEventListener("resize", queuePlace);
	window.addEventListener("scroll", queuePlace, true);

	function items(menu) {
		return Array.prototype.slice.call(menu.querySelectorAll("[data-ui-menuitem]")).filter(function (it) {
			return !it.disabled && it.getAttribute("aria-disabled") !== "true";
		});
	}

	function focusItem(menu, which) {
		var list = items(menu);
		if (!list.length) {
			menu.focus();
			return;
		}
		var i = list.indexOf(document.activeElement);
		var next;
		if (which === "first") next = 0;
		else if (which === "last") next = list.length - 1;
		else if (which === "next") next = i < 0 ? 0 : (i + 1) % list.length;
		else next = i < 0 ? list.length - 1 : (i - 1 + list.length) % list.length;
		list[next].focus();
	}

	var FOCUSABLE = "a[href], button:not([disabled]):not([data-ui-popover-close]), input:not([type=hidden]):not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

	function setExpanded(p, open) {
		invokers(p).forEach(function (t) {
			t.setAttribute("aria-expanded", open ? "true" : "false");
		});
	}

	function showOverlay(p, anchor, focusWhich) {
		if (!p || !p.isConnected) return;
		if (anchor) p._uiAnchor = anchor;
		p._uiFocus = focusWhich || "first";
		if (!isOpen(p)) p.showPopover();
		else place(p);
	}

	function hideOverlay(p, refocus) {
		if (!p || !isOpen(p)) return;
		p._uiRefocus = !!refocus;
		p.hidePopover();
	}

	// beforetoggle (open): the panel is not laid out yet, so start it right
	// under the anchor; toggle refines it once it has a size. Both events do
	// not bubble, so listen in the capture phase.
	document.addEventListener(
		"beforetoggle",
		function (evt) {
			var p = evt.target;
			if (!p || !p.matches || !p.matches(OVERLAY)) return;
			if (evt.newState === "open") {
				var a = anchorOf(p);
				if (a && p.getAttribute("data-ui-overlay") !== "tooltip") {
					var r = a.getBoundingClientRect();
					p.style.top = Math.round(r.bottom + GAP) + "px";
					p.style.left = Math.round(r.left) + "px";
					p.setAttribute("data-ui-placed", "bottom");
				}
			}
		},
		true,
	);

	document.addEventListener(
		"toggle",
		function (evt) {
			var p = evt.target;
			if (!p || !p.matches || !p.matches(OVERLAY)) return;
			var kind = p.getAttribute("data-ui-overlay");
			var open = evt.newState === "open";
			if (kind === "tooltip") {
				if (open) place(p);
				return;
			}
			setExpanded(p, open);
			if (open) {
				place(p);
				var which = p._uiFocus || "first";
				p._uiFocus = null;
				if (kind === "menu") focusItem(p, which);
				else {
					var first = p.querySelector(FOCUSABLE);
					(first || p).focus();
				}
				return;
			}
			// Focus goes back to the trigger after Escape, Tab, or a chosen
			// item, and whenever it was inside the panel or fell to <body>.
			// A light-dismiss click on another control keeps focus there.
			var refocus = p._uiRefocus;
			p._uiRefocus = false;
			var a = anchorOf(p);
			p._uiAnchor = null;
			var act = document.activeElement;
			if (a && a.focus && (refocus || !act || act === document.body || p.contains(act))) a.focus();
		},
		true,
	);

	// A click on a trigger records it as the anchor before the browser's
	// popovertarget behavior opens the panel.
	document.addEventListener(
		"click",
		function (evt) {
			var t = evt.target && evt.target.closest && evt.target.closest("[popovertarget]");
			if (t && t.getAttribute("popovertargetaction") !== "hide") {
				var p = document.getElementById(t.getAttribute("popovertarget"));
				if (p && p.matches(OVERLAY)) {
					p._uiAnchor = t;
					p._uiFocus = "first";
				}
			}
		},
		true,
	);

	// Choosing a menu item closes the menu (bubble phase, after htmx saw the
	// click). Links navigate and htmx buttons send their request as usual;
	// an item that opens a dialog leaves focus to the dialog.
	document.addEventListener("click", function (evt) {
		var item = evt.target && evt.target.closest && evt.target.closest("[data-ui-menuitem]");
		if (!item) return;
		var menu = item.closest("[data-ui-menu]");
		if (menu) hideOverlay(menu, item.getAttribute("aria-haspopup") !== "dialog");
	});

	var typeahead = "";
	var typeaheadTimer = 0;

	function menuKeydown(evt, menu) {
		var k = evt.key;
		if (k === "ArrowDown") focusItem(menu, "next");
		else if (k === "ArrowUp") focusItem(menu, "prev");
		else if (k === "Home" || k === "PageUp") focusItem(menu, "first");
		else if (k === "End" || k === "PageDown") focusItem(menu, "last");
		else if (k === "Escape") hideOverlay(menu, true);
		else if (k === "Tab") {
			// Close and move on from the trigger: focus it now (toggle fires
			// too late) and skip preventDefault, so the browser's Tab runs
			// from the trigger.
			var a = anchorOf(menu);
			if (a && a.isConnected) a.focus();
			hideOverlay(menu, !a);
			return;
		} else if (k === " " || k === "Spacebar") {
			var cur = document.activeElement;
			if (cur && cur.matches("[data-ui-menuitem]")) cur.click();
		} else if (k.length === 1 && !evt.ctrlKey && !evt.metaKey && !evt.altKey) {
			clearTimeout(typeaheadTimer);
			typeahead += k.toLowerCase();
			typeaheadTimer = setTimeout(function () {
				typeahead = "";
			}, 500);
			var list = items(menu);
			var start = Math.max(0, list.indexOf(document.activeElement));
			for (var n = 1; n <= list.length; n++) {
				// A single repeated key cycles through matches; a longer
				// prefix searches from the current item.
				var off = typeahead.length === 1 ? n : n - 1;
				var it = list[(start + off) % list.length];
				if (it.textContent.trim().toLowerCase().indexOf(typeahead) === 0) {
					it.focus();
					break;
				}
			}
		} else return;
		evt.preventDefault();
	}

	document.addEventListener("keydown", function (evt) {
		var t = evt.target;
		if (!t || !t.closest) return;
		// Escape hides an open tooltip first, without moving focus or
		// closing the dialog or menu around it.
		if (evt.key === "Escape" && openTip) {
			hideTip(true);
			evt.preventDefault();
			evt.stopPropagation();
			return;
		}
		var menu = t.closest("[data-ui-menu]");
		if (menu && isOpen(menu)) {
			menuKeydown(evt, menu);
			return;
		}
		var trigger = t.closest("[data-ui-menu-trigger]");
		if (trigger && (evt.key === "ArrowDown" || evt.key === "ArrowUp")) {
			var m = document.getElementById(trigger.getAttribute("popovertarget"));
			if (!m) return;
			evt.preventDefault();
			var which = evt.key === "ArrowUp" ? "last" : "first";
			if (isOpen(m)) focusItem(m, which);
			else showOverlay(m, trigger, which);
			return;
		}
		var pop = t.closest('[data-ui-overlay="popover"]');
		if (pop && evt.key === "Escape" && isOpen(pop)) {
			evt.preventDefault();
			hideOverlay(pop, true);
		}
	});

	// Pointer focus follows the mouse inside an open menu, like a native menu.
	document.addEventListener("pointermove", function (evt) {
		if (evt.pointerType === "touch") return;
		var item = evt.target && evt.target.closest && evt.target.closest("[data-ui-menuitem]");
		if (item && item !== document.activeElement && isOpen(item.closest("[data-ui-menu]"))) item.focus({ preventScroll: true });
	});

	// Tooltips. One is open at a time.
	var openTip = null;
	var tipShowTimer = 0;
	var tipHideTimer = 0;
	var tipSuppressed = null;

	function tipFor(trigger) {
		var id = trigger.getAttribute("data-ui-tooltip");
		var tip = id && document.getElementById(id);
		return tip && tip.matches('[data-ui-overlay="tooltip"]') ? tip : null;
	}

	function showTip(trigger) {
		var tip = tipFor(trigger);
		if (!tip || !trigger.isConnected) return;
		if (openTip && openTip !== tip) hideTip(false);
		clearTimeout(tipHideTimer);
		tip._uiAnchor = trigger;
		if (!isOpen(tip)) tip.showPopover();
		place(tip);
		openTip = tip;
	}

	function hideTip(suppress) {
		clearTimeout(tipShowTimer);
		clearTimeout(tipHideTimer);
		var tip = openTip;
		openTip = null;
		if (!tip) return;
		if (suppress) tipSuppressed = tip._uiAnchor;
		if (isOpen(tip)) tip.hidePopover();
	}

	function scheduleTip(trigger) {
		if (trigger === tipSuppressed) return;
		clearTimeout(tipHideTimer);
		clearTimeout(tipShowTimer);
		var tip = tipFor(trigger);
		if (tip && tip === openTip) return;
		tipShowTimer = setTimeout(function () {
			showTip(trigger);
		}, TIP_SHOW_MS);
	}

	function scheduleHide() {
		clearTimeout(tipShowTimer);
		clearTimeout(tipHideTimer);
		tipHideTimer = setTimeout(function () {
			hideTip(false);
		}, TIP_HIDE_MS);
	}

	// Hovering the trigger or the open tooltip keeps it; leaving both hides it.
	function overTip(node) {
		return !!(openTip && node && (openTip.contains(node) || (openTip._uiAnchor && openTip._uiAnchor.contains(node))));
	}

	document.addEventListener("pointerover", function (evt) {
		if (evt.pointerType === "touch") return;
		var t = evt.target;
		if (!t || !t.closest) return;
		if (overTip(t)) {
			clearTimeout(tipHideTimer);
			return;
		}
		var trigger = t.closest("[data-ui-tooltip]");
		if (trigger) scheduleTip(trigger);
	});

	document.addEventListener("pointerout", function (evt) {
		var t = evt.target;
		if (!t || !t.closest) return;
		var trigger = t.closest("[data-ui-tooltip]");
		if (trigger && !trigger.contains(evt.relatedTarget) && trigger === tipSuppressed) tipSuppressed = null;
		if (overTip(evt.relatedTarget)) return;
		if (trigger || (openTip && openTip.contains(t))) scheduleHide();
	});

	document.addEventListener("focusin", function (evt) {
		var t = evt.target;
		var trigger = t && t.closest && t.closest("[data-ui-tooltip]");
		if (trigger) {
			tipSuppressed = null;
			scheduleTip(trigger);
		}
	});

	document.addEventListener("focusout", function (evt) {
		var t = evt.target;
		var trigger = t && t.closest && t.closest("[data-ui-tooltip]");
		if (!trigger) return;
		// Leaving focus ends an Escape dismissal, like leaving hover does.
		if (trigger === tipSuppressed && !trigger.matches(":hover")) tipSuppressed = null;
		if (openTip && openTip._uiAnchor === trigger && trigger.matches(":hover")) return;
		hideTip(false);
	});

	// An htmx swap that replaces an open overlay's anchor or the overlay
	// itself closes it first, so focus returns while the trigger exists.
	document.addEventListener("htmx:beforeSwap", function (evt) {
		var target = evt.detail && evt.detail.target;
		if (!target || !target.contains) return;
		document.querySelectorAll(OVERLAY).forEach(function (p) {
			if (!isOpen(p)) return;
			var a = anchorOf(p);
			if (target.contains(p) || (a && target.contains(a))) {
				if (p === openTip) hideTip(false);
				else hideOverlay(p, false);
			}
		});
	});

	// --- Copy (ui.CopyAttrs) --------------------------------------------------

	// A button with data-ui-copy="<id>" copies the text of that element and
	// reads "Copied!" (or "Copy failed") for two seconds, then its own label
	// again. The copied text stays in the page, never in an attribute, so a
	// secret is not duplicated into the markup.
	var COPY_MS = 2000;

	document.addEventListener("click", function (evt) {
		var btn = evt.target && evt.target.closest && evt.target.closest("[data-ui-copy]");
		if (!btn) return;
		var src = document.getElementById(btn.getAttribute("data-ui-copy"));
		if (!src) return;
		if (!btn.hasAttribute("data-ui-copy-label")) btn.setAttribute("data-ui-copy-label", btn.textContent.trim());
		var label = btn.getAttribute("data-ui-copy-label");
		function flash(text) {
			btn.textContent = text;
			clearTimeout(btn._uiCopyTimer);
			btn._uiCopyTimer = setTimeout(function () {
				btn.textContent = label;
			}, COPY_MS);
		}
		var write =
			navigator.clipboard && navigator.clipboard.writeText
				? navigator.clipboard.writeText(src.textContent)
				: Promise.reject(new Error("clipboard unavailable"));
		write.then(
			function () {
				flash("Copied!");
			},
			function () {
				flash("Copy failed");
			}
		);
	});

	function init() {
		initTree(document);
		initDialogs(document);
		initSheets(document);
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
		place: place,
		openDialog: openDialog,
		confirm: askConfirm,
	};
})();
