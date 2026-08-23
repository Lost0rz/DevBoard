(function () {
  "use strict";
  var container = document.getElementById("dashboard-dynamic-container");
  var warning = document.getElementById("live-refresh-warning");
  if (!container) return;
  var seconds = Number(container.getAttribute("data-refresh-seconds"));
  if (!Number.isFinite(seconds) || seconds <= 0) return;
  function refresh() {
    fetch("/display/fragment", { method: "GET", cache: "no-store", credentials: "same-origin" })
      .then(function (response) {
        if (!response.ok) throw new Error("fragment request failed");
        return response.text();
      })
      .then(function (html) {
        container.innerHTML = html;
        if (warning) warning.hidden = true;
      })
      .catch(function () {
        if (warning) warning.hidden = false;
      });
  }
  window.setInterval(refresh, seconds * 1000);
}());
