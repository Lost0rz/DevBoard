(function () {
  "use strict";

  var container = document.getElementById("dashboard-dynamic-container");
  if (!container) return;

  var seconds = Number(container.getAttribute("data-refresh-seconds"));
  if (!Number.isFinite(seconds) || seconds <= 0) return;

  var fragmentPath = container.getAttribute("data-fragment-path") || "/display/fragment";

  var delay = seconds * 1000;
  var timer = null;
  var refreshing = false;

  function setRefreshPaused(paused) {
    container.setAttribute("data-refresh-state", paused ? "paused" : "live");
    var strip = document.querySelector("[data-pad-connection-strip]");
    var status = document.querySelector("[data-pad-refresh-status]");
    if (strip) strip.setAttribute("data-refresh-state", paused ? "stale" : "live");
    if (status) status.textContent = paused ? "REFRESH STALE" : "REFRESH LIVE";
  }

  function schedule() {
    window.clearTimeout(timer);
    timer = window.setTimeout(refresh, delay);
  }

  function refresh() {
    if (refreshing) return;
    refreshing = true;

    fetch(fragmentPath, {
      method: "GET",
      cache: "no-store",
      credentials: "same-origin",
      headers: { "X-DevBoard-Fragment": "1" }
    })
      .then(function (response) {
        if (!response.ok) throw new Error("fragment request failed");
        return response.text();
      })
      .then(function (html) {
        if (!html.trim()) throw new Error("fragment response empty");
        container.innerHTML = html;
        container.setAttribute("data-last-refresh", new Date().toISOString());
        setRefreshPaused(false);
      })
      .catch(function () {
        // Keep the last successful server-rendered DOM visible. A later
        // successful request replaces it and clears the strip marker.
        setRefreshPaused(true);
      })
      .finally(function () {
        refreshing = false;
        schedule();
      });
  }

  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      window.clearTimeout(timer);
      return;
    }
    refresh();
  });

  setRefreshPaused(false);
  schedule();
}());
