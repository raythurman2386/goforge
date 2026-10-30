// GoForge UI kit runtime (components/ui). Loaded from layouts.Base after
// app.js. No build step; plain ES2017 like app.js.
//
// PR 1 shipped the delegated listener pattern and the htmx:load init pass.
// PR 2 added the dismissible Alert. PR 4 adds Dialog and the hx-confirm
// dialog. Later PRs register Dropdown, Popover, Tooltip, Tabs, and Sheet. Rules, from the UI kit
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

	function openDialog(d, opener) {
		if (!d || d.open || !d.isConnected) return;
		d._uiOpener = opener || document.activeElement;
		d.showModal();
		var first = d.querySelector(FOCUS_FIRST);
		if (first) first.focus();
	}

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
			var back = liveTwin(d._uiOpener);
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

	function init() {
		initTree(document);
		initDialogs(document);
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
		openDialog: openDialog,
		confirm: askConfirm,
	};
})();
