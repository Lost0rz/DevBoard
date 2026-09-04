(function () {
  "use strict";

  var container = document.getElementById("dashboard-dynamic-container");
  if (!container) return;

  var seconds = Number(container.getAttribute("data-refresh-seconds"));
  if (!Number.isFinite(seconds) || seconds <= 0) return;

	var fragmentPath = container.getAttribute("data-fragment-path") || "/display/fragment";

	var delay = seconds * 1000;
	// A stalled fragment request must not permanently hold refreshing=true.
	// Keep the watchdog bounded so a background network/socket problem can
	// recover on the next poll without requiring a page reload.
	var refreshTimeout = Math.max(5000, Math.min(15000, delay * 3));
	var timer = null;
	var refreshing = false;
	var activeController = null;

  function setRefreshPaused(paused) {
    container.setAttribute("data-refresh-state", paused ? "paused" : "live");
    var strip = document.querySelector("[data-pad-connection-strip]");
    var status = document.querySelector("[data-pad-refresh-status]");
    if (strip) strip.setAttribute("data-refresh-state", paused ? "stale" : "live");
    if (status) status.textContent = paused ? "REFRESH STALE" : "REFRESH LIVE";
  }

	function schedule() {
		window.clearTimeout(timer);
		timer = null;
		if (document.hidden) return;
		timer = window.setTimeout(refresh, delay);
	}

	function refresh() {
		if (refreshing || document.hidden) return;
		refreshing = true;
		var controller = typeof AbortController === "function" ? new AbortController() : null;
		activeController = controller;
		var timedOut = false;
		var watchdog = null;
		var request = {
			method: "GET",
			cache: "no-store",
			credentials: "same-origin",
			headers: { "X-DevBoard-Fragment": "1" }
		};
		if (controller) request.signal = controller.signal;

		var timeout = new Promise(function (_, reject) {
			watchdog = window.setTimeout(function () {
				timedOut = true;
				if (controller) controller.abort();
				reject(new Error("fragment request timed out"));
			}, refreshTimeout);
		});
		var response = Promise.resolve().then(function () {
			return fetch(fragmentPath, request);
		}).then(function (response) {
			if (!response.ok) throw new Error("fragment request failed");
			return response.text();
		});

		Promise.race([response, timeout])
			.then(function (html) {
				if (timedOut) return;
				if (!html.trim()) throw new Error("fragment response empty");
				container.innerHTML = html;
				container.setAttribute("data-last-refresh", new Date().toISOString());
				setRefreshPaused(false);
			})
			.catch(function () {
				// Keep the last successful server-rendered DOM visible. A later
				// successful request replaces it and clears the strip marker.
				if (!document.hidden) setRefreshPaused(true);
			})
			.finally(function () {
				window.clearTimeout(watchdog);
				if (activeController === controller) activeController = null;
				refreshing = false;
				schedule();
			});
	}

	document.addEventListener("visibilitychange", function () {
		if (document.hidden) {
			window.clearTimeout(timer);
			timer = null;
			if (activeController) activeController.abort();
			return;
		}
		refresh();
	});

  setRefreshPaused(false);
  schedule();
}());
