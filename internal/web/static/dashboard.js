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
	var activeWatchdog = null;
	var requestGeneration = 0;
	var refreshStartedAt = 0;
	var lastSuccessfulRefreshAt = Date.now();
	var recoveryInterval = Math.max(1000, Math.min(5000, refreshTimeout));

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
		refreshStartedAt = Date.now();
		var requestId = ++requestGeneration;
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
		activeWatchdog = watchdog;
		var response = Promise.resolve().then(function () {
			return fetch(fragmentPath, request);
		}).then(function (response) {
			if (!response.ok) throw new Error("fragment request failed");
			return response.text();
		});

		Promise.race([response, timeout])
			.then(function (html) {
				if (requestId !== requestGeneration) return;
				if (timedOut) return;
				if (!html.trim()) throw new Error("fragment response empty");
				container.innerHTML = html;
				container.setAttribute("data-last-refresh", new Date().toISOString());
				lastSuccessfulRefreshAt = Date.now();
				setRefreshPaused(false);
			})
			.catch(function () {
				if (requestId !== requestGeneration) return;
				// Keep the last successful server-rendered DOM visible. A later
				// successful request replaces it and clears the strip marker.
				if (!document.hidden) setRefreshPaused(true);
			})
			.finally(function () {
				if (requestId !== requestGeneration) return;
				window.clearTimeout(watchdog);
				if (activeWatchdog === watchdog) activeWatchdog = null;
				if (activeController === controller) activeController = null;
				refreshing = false;
				refreshStartedAt = 0;
				schedule();
			});
	}

	function recoverIfStalled() {
		if (document.hidden) return false;
		var now = Date.now();
		if (refreshing) {
			if (!refreshStartedAt || now - refreshStartedAt < refreshTimeout) return false;
			// A suspended browser can postpone both fetch completion and the
			// JavaScript timeout. Invalidate the old request so it cannot keep
			// the page permanently locked in refreshing=true.
			requestGeneration += 1;
			if (activeController) activeController.abort();
			if (activeWatchdog) window.clearTimeout(activeWatchdog);
			activeController = null;
			activeWatchdog = null;
			refreshing = false;
			refreshStartedAt = 0;
			setRefreshPaused(true);
		}
		if (now - lastSuccessfulRefreshAt < Math.max(refreshTimeout, delay * 2)) return false;
		window.clearTimeout(timer);
		timer = null;
		refresh();
		return true;
	}

	function refreshOnResume() {
		if (document.hidden) return;
		if (recoverIfStalled()) return;
		if (!refreshing) {
			window.clearTimeout(timer);
			timer = null;
			refresh();
		}
	}

	document.addEventListener("visibilitychange", function () {
		if (document.hidden) {
			window.clearTimeout(timer);
			timer = null;
			if (activeController) activeController.abort();
			return;
		}
		refreshOnResume();
	});
	window.addEventListener("pageshow", refreshOnResume);
	window.addEventListener("focus", refreshOnResume);
	window.addEventListener("online", refreshOnResume);
	window.setInterval(recoverIfStalled, recoveryInterval);

  setRefreshPaused(false);
  schedule();
}());
