/* Bounded Operator Console refresh. The /display contract uses dashboard.js
 * and is deliberately not touched by this controller. */
(function () {
  "use strict";

  var root = document.querySelector("[data-admin-page]");
  if (!root) return;

  var page = root.dataset.adminPage || "";
  var disabled = root.dataset.refreshDisabled === "true";
  var configuredSeconds = Number(root.dataset.refreshSeconds || "10");
  var intervalMs = Math.max(5000, Math.min(60000, configuredSeconds * 1000));
  var state = {
    enabled: !disabled && (page === "overview" || page === "logs" || page === "nodes"),
    intervalMs: intervalMs,
    inFlight: false,
    refreshCount: 0,
    lastRefreshAt: null,
    stopped: false,
    hidden: false,
    timerActive: false
  };
  var timer = null;
  var activeController = null;
  var resumePending = false;

  function currentRoot() {
    return root && root.isConnected ? root : document.querySelector("[data-admin-page]");
  }

  function editingNodes() {
    var nodeRoot = currentRoot();
    if (!nodeRoot || page !== "nodes") return false;
    var form = nodeRoot.querySelector("form[data-preserve-refresh]");
    if (!form) return false;
    return form.dataset.dirty === "true" || form.contains(document.activeElement);
  }

  function replaceNodesStatus(nextRoot) {
    var current = currentRoot();
    var currentRegion = current && current.querySelector('[data-refresh-region="nodes-status"]');
    var nextRegion = nextRoot.querySelector('[data-refresh-region="nodes-status"]');
    if (currentRegion && nextRegion) currentRegion.replaceWith(nextRegion);
  }

  async function refresh() {
    if (!state.enabled || state.stopped || state.hidden || state.inFlight) return false;
    state.inFlight = true;
    var controller = typeof AbortController === "function" ? new AbortController() : null;
    activeController = controller;
    var timeout = setTimeout(function () {
      if (controller) controller.abort();
    }, Math.min(intervalMs, 15000));
    try {
      var response = await fetch(window.location.href, {
        headers: { "X-DevBoard-Refresh": "1" },
        cache: "no-store",
        signal: controller ? controller.signal : undefined
      });
      if (!response.ok) throw new Error("refresh unavailable");
      var html = await response.text();
      var parsed = new DOMParser().parseFromString(html, "text/html");
      var nextRoot = parsed.querySelector("[data-admin-page]");
      if (!nextRoot || nextRoot.dataset.adminPage !== page) throw new Error("refresh response invalid");
      if (page === "nodes" && editingNodes()) replaceNodesStatus(nextRoot);
      else {
        var current = currentRoot();
        if (current) current.replaceWith(nextRoot);
        root = nextRoot;
      }
      state.refreshCount += 1;
      state.lastRefreshAt = Date.now();
      return true;
    } catch (_) {
      // A failed refresh leaves the current page intact. The server-rendered
      // state remains authoritative and the next bounded tick can retry.
      return false;
    } finally {
      clearTimeout(timeout);
      if (activeController === controller) activeController = null;
      state.inFlight = false;
      if (resumePending && !state.hidden && !state.stopped) {
        resumePending = false;
        startTimer();
        refresh();
      }
    }
  }

  function startTimer() {
    if (!state.enabled || state.stopped || state.hidden || timer !== null) return;
    timer = setInterval(refresh, intervalMs);
    state.timerActive = true;
  }

  function pauseTimer() {
    if (timer !== null) clearInterval(timer);
    timer = null;
    state.timerActive = false;
  }

  function pauseForHidden() {
    if (state.stopped) return;
    state.hidden = true;
    pauseTimer();
    if (activeController) activeController.abort();
  }

  function resumeFromHidden() {
    if (state.stopped || !state.hidden) return;
    state.hidden = false;
    if (state.inFlight) {
      resumePending = true;
      return;
    }
    startTimer();
    refresh();
  }

  function stop() {
    state.stopped = true;
    pauseTimer();
    if (activeController) activeController.abort();
  }

  document.addEventListener("input", function (event) {
    var target = event.target;
    if (target && target.closest) {
      var form = target.closest("form[data-preserve-refresh]");
      if (form) form.dataset.dirty = "true";
    }
  });

  window.DevBoardAdminRefresh = {
    state: state,
    refresh: refresh,
    stop: stop,
    intervalMs: intervalMs
  };

  if (!state.enabled) return;
  window.addEventListener("pagehide", stop, { once: true });
  window.addEventListener("beforeunload", stop, { once: true });
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") pauseForHidden();
    else resumeFromHidden();
  });
  startTimer();
  // The initial document is already server-rendered. This immediate, bounded
  // reconciliation makes an Overview/Logs open converge without waiting for
  // the first interval and is guarded by the in-flight latch.
  refresh();
}());
