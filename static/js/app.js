// GoForge shell behavior: sidebar active link, toasts, and HTMX error toasts.
// Loaded from layouts.Base after htmx. No build step; plain ES2017.
(function () {
	"use strict";

	var TOAST_MS = 5000;
	var MAX_MESSAGE = 300;
	var LEVEL_CLASSES = {
		success: "bg-green-500 text-white",
		error: "bg-red-500 text-white",
		info: "bg-blue-500 text-white",
	};

	// --- Toasts --------------------------------------------------------------

	function toastRegion() {
		return document.getElementById("toast");
	}

	function scheduleDismiss(el) {
		if (el.nodeType !== 1 || el.dataset.toastTimer) return;
		el.dataset.toastTimer = "1";
		setTimeout(function () {
			el.remove();
		}, TOAST_MS);
	}

	function showToast(message, level) {
		var region = toastRegion();
		if (!region || !message) return;
		// Do not stack the same message twice.
		for (var i = 0; i < region.children.length; i++) {
			if (region.children[i].dataset.toastMessage === message) return;
		}
		var el = document.createElement("div");
		el.setAttribute("data-toast", "");
		el.dataset.toastMessage = message;
		if (level === "error") el.setAttribute("role", "alert");
		el.className =
			"rounded-md p-4 shadow-lg flex items-center space-x-3 " +
			(LEVEL_CLASSES[level] || LEVEL_CLASSES.info);

		var text = document.createElement("span");
		text.textContent = message;
		var close = document.createElement("button");
		close.type = "button";
		close.setAttribute("aria-label", "Dismiss");
		close.className = "text-white hover:text-gray-200";
		close.textContent = "\u00d7";
		close.addEventListener("click", function () {
			el.remove();
		});

		el.appendChild(text);
		el.appendChild(close);
		region.appendChild(el);
	}

	// Auto-dismiss every toast that lands in the region, whether app.js made
	// it or the server swapped it in with components.Toast (hx-swap-oob).
	function watchToastRegion() {
		var region = toastRegion();
		if (!region) return;
		Array.prototype.forEach.call(region.children, scheduleDismiss);
		new MutationObserver(function (mutations) {
			mutations.forEach(function (m) {
				Array.prototype.forEach.call(m.addedNodes, scheduleDismiss);
			});
		}).observe(region, { childList: true });
	}

	// --- HTMX error routing -------------------------------------------------

	// Turn an error response into one line of text. RenderError sends a small
	// HTML fragment for HTMX requests; plain-text errors pass through.
	function errorMessage(xhr) {
		var status = xhr ? xhr.status : 0;
		var fallback = "Request failed" + (status ? " (" + status + ")" : "") + ".";
		var body = (xhr && xhr.responseText) || "";
		if (!body || /<html[\s>]/i.test(body)) return fallback;
		var doc = new DOMParser().parseFromString(body, "text/html");
		doc.querySelectorAll("script, style").forEach(function (n) {
			n.remove();
		});
		var text = (doc.body ? doc.body.textContent : body).replace(/\s+/g, " ").trim();
		if (!text) return fallback;
		if (text.length > MAX_MESSAGE) text = text.slice(0, MAX_MESSAGE - 1) + "\u2026";
		return text;
	}

	// Requests from inside the modal show errors in the modal (components.Modal).
	// Polling requests would toast every few seconds, so they stay quiet.
	function shouldToast(elt) {
		if (!elt || !elt.closest) return true;
		if (elt.closest("#modal")) return false;
		var trigger = elt.getAttribute("hx-trigger") || "";
		return !/\bevery\b/.test(trigger);
	}

	// htmx 2 does not swap 4xx/5xx responses by default. Rather than change
	// htmx.config.responseHandling (which would let an error fragment replace
	// a polling target such as #instance-list), surface the text as a toast.
	document.addEventListener("htmx:responseError", function (evt) {
		if (!shouldToast(evt.detail.elt)) return;
		showToast(errorMessage(evt.detail.xhr), "error");
	});

	document.addEventListener("htmx:sendError", function (evt) {
		if (!shouldToast(evt.detail.elt)) return;
		showToast("Could not reach GoForge. Check your connection and try again.", "error");
	});

	// --- Sidebar active link ------------------------------------------------

	// Mark the [data-nav] link with the longest prefix match for the current
	// path, so /projects/123/deployments/456 highlights Projects.
	function markActiveNav() {
		var path = window.location.pathname;
		var best = null;
		var bestLen = -1;
		document.querySelectorAll("[data-nav]").forEach(function (a) {
			a.removeAttribute("aria-current");
			var prefix = a.getAttribute("data-nav");
			if ((path === prefix || path.indexOf(prefix + "/") === 0) && prefix.length > bestLen) {
				best = a;
				bestLen = prefix.length;
			}
		});
		if (best) best.setAttribute("aria-current", "page");
	}

	function init() {
		markActiveNav();
		watchToastRegion();
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", init);
	} else {
		init();
	}

	window.GoForge = window.GoForge || {};
	window.GoForge.toast = showToast;
})();
