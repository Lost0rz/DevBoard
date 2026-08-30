/* Bounded Operator Console refresh. The /display contract uses dashboard.js
 * and is deliberately not touched by this controller. */
(function () {
  "use strict";

  var root = document.querySelector("[data-admin-page]");
  if (!root) return;

  var page = root.dataset.adminPage || "";
  var disabled = root.dataset.refreshDisabled === "true";
  var settingsRoute = window.location && window.location.pathname === "/admin/settings";
  var configuredSeconds = Number(root.dataset.refreshSeconds || "10");
  var intervalMs = Math.max(5000, Math.min(60000, configuredSeconds * 1000));
  var state = {
    enabled: !disabled && !settingsRoute && (page === "overview" || page === "logs" || page === "nodes" || page === "console"),
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

  function editingForm() {
    var nodeRoot = currentRoot();
    if (!nodeRoot) return false;
    var forms = typeof nodeRoot.querySelectorAll === "function" ? nodeRoot.querySelectorAll("form[data-preserve-refresh]") : null;
    if (forms && forms.length) {
      for (var i = 0; i < forms.length; i += 1) {
        if (forms[i].dataset.dirty === "true" || forms[i].contains(document.activeElement)) return true;
      }
      return false;
    }
    var form = nodeRoot.querySelector("form[data-preserve-refresh]");
    return !!form && (form.dataset.dirty === "true" || form.contains(document.activeElement));
  }

  async function refresh() {
    // Settings forms are server-rendered as one page. A polling replacement
    // must never erase a value while an operator is typing or has already
    // changed a field, regardless of which form appears first in the page.
    if (!state.enabled || state.stopped || state.hidden || state.inFlight || editingForm()) return false;
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
      var current = currentRoot();
      if (current) current.replaceWith(nextRoot);
      root = nextRoot;
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

  function initAgentQuotaTest() {
    var form = document.querySelector("[data-agent-quota-test]");
    if (form) {
      form.addEventListener("submit", function () {
        var button = form.querySelector("button[type='submit']");
        var status = form.querySelector("[data-agent-quota-test-status]");
        if (button) {
          button.disabled = true;
          button.textContent = "Waiting for response…";
        }
        if (status) status.textContent = "Waiting for the detailed provider response…";
        form.setAttribute("aria-busy", "true");
      });
    }
    var result = document.querySelector("[data-agent-quota-test-result]");
    if (result && result.dataset.hasResult === "true") {
      // Keep the received response visible until the operator navigates away.
      // A background overview refresh must not replace it with a blank GET.
      window.DevBoardAdminRefresh.stop();
    }
  }

  function initScheduleEditors() {
    if (typeof document.querySelectorAll !== "function") return;
    var editors = document.querySelectorAll("[data-schedule-editor]");
    for (var editorIndex = 0; editorIndex < editors.length; editorIndex += 1) {
      (function (editor) {
        var input = editor.querySelector("[data-schedule-input]");
        var add = editor.querySelector("[data-schedule-add]");
        var list = editor.querySelector("[data-schedule-list]");
        if (!input || !add || !list) return;

        function markDirty() {
          var form = editor.closest("form[data-preserve-refresh]");
          if (form) form.dataset.dirty = "true";
        }

        function emptyState() {
          var hasItems = list.querySelector("[data-schedule-item]");
          var empty = list.querySelector("[data-schedule-empty]");
          if (!hasItems && !empty) {
            empty = document.createElement("li");
            empty.className = "schedule-empty";
            empty.dataset.scheduleEmpty = "";
            empty.textContent = "No activation times added yet.";
            list.appendChild(empty);
          } else if (hasItems && empty) {
            empty.remove();
          }
        }

        function createItem(value) {
          var item = document.createElement("li");
          item.className = "schedule-item";
          item.dataset.scheduleItem = "";

          var label = document.createElement("label");
          label.className = "schedule-time";
          var timeInput = document.createElement("input");
          timeInput.type = "time";
          timeInput.name = "agent_quota_schedule";
          timeInput.value = value;
          timeInput.step = "60";
          timeInput.required = true;
          timeInput.setAttribute("aria-label", "Activation time " + value);
          label.appendChild(timeInput);

          var actions = document.createElement("div");
          actions.className = "schedule-actions";
          [["↑", "scheduleUp", "Move " + value + " up"], ["↓", "scheduleDown", "Move " + value + " down"], ["Remove", "scheduleRemove", "Remove " + value]].forEach(function (definition) {
            var button = document.createElement("button");
            button.type = "button";
            button.className = definition[1] === "scheduleRemove" ? "button-danger small" : "button-secondary small";
            button.dataset[definition[1]] = "";
            button.setAttribute("aria-label", definition[2]);
            button.textContent = definition[0];
            actions.appendChild(button);
          });
          item.appendChild(label);
          item.appendChild(actions);

          function updateActionLabels() {
            var current = timeInput.value || "activation time";
            var buttons = actions.querySelectorAll("button");
            for (var index = 0; index < buttons.length; index += 1) {
              if (buttons[index].dataset.scheduleUp !== undefined) buttons[index].setAttribute("aria-label", "Move " + current + " up");
              if (buttons[index].dataset.scheduleDown !== undefined) buttons[index].setAttribute("aria-label", "Move " + current + " down");
              if (buttons[index].dataset.scheduleRemove !== undefined) buttons[index].setAttribute("aria-label", "Remove " + current);
            }
            timeInput.setAttribute("aria-label", "Activation time " + current);
          }

          timeInput.addEventListener("input", function () {
            updateActionLabels();
            markDirty();
          });
          return item;
        }

        add.addEventListener("click", function () {
          var value = (input.value || "").trim();
          if (!/^([01]\\d|2[0-3]):[0-5]\\d$/.test(value)) {
            input.focus();
            return;
          }
          var existing = list.querySelectorAll("input[name='agent_quota_schedule']");
          for (var i = 0; i < existing.length; i += 1) {
            if (existing[i].value === value) {
              input.focus();
              return;
            }
          }
          var empty = list.querySelector("[data-schedule-empty]");
          if (empty) empty.remove();
          list.appendChild(createItem(value));
          input.value = "";
          markDirty();
        });

        editor.addEventListener("click", function (event) {
          var button = event.target.closest ? event.target.closest("button") : null;
          var item = button && button.closest ? button.closest("[data-schedule-item]") : null;
          if (!button || !item) return;
          if (button.dataset.scheduleRemove !== undefined) {
            item.remove();
          } else if (button.dataset.scheduleUp !== undefined && item.previousElementSibling && item.previousElementSibling.matches("[data-schedule-item]")) {
            list.insertBefore(item, item.previousElementSibling);
          } else if (button.dataset.scheduleDown !== undefined && item.nextElementSibling && item.nextElementSibling.matches("[data-schedule-item]")) {
            list.insertBefore(item.nextElementSibling, item);
          } else {
            return;
          }
          emptyState();
          markDirty();
        });
      }(editors[editorIndex]));
    }
  }

  initAgentQuotaTest();
  initScheduleEditors();

  if (!state.enabled) return;
  window.addEventListener("pagehide", stop, { once: true });
  window.addEventListener("beforeunload", stop, { once: true });
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") pauseForHidden();
    else resumeFromHidden();
  });
  startTimer();
  // The initial document is already server-rendered. This immediate, bounded
  // reconciliation makes a Dashboard/Diagnostics open converge without waiting for
  // the first interval and is guarded by the in-flight latch.
  refresh();
}());
