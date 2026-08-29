package web

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAdminRefreshControllerBehavior(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	source, err := templateFS.ReadFile("static/admin.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(string(source))
	if err != nil {
		t.Fatal(err)
	}
	const harness = `
const vm = require("vm");
const assert = require("assert");

function rootFor(page, document, opts) {
  const form = { dataset: {}, contains: node => node === form };
  const region = { replaced: false, replaceWith(next) { this.replaced = true; this.next = next; } };
  const root = {
    dataset: { adminPage: page, refreshSeconds: String((opts && opts.seconds) || 8), refreshDisabled: (opts && opts.disabled) ? "true" : "false" },
    isConnected: true,
    replaced: false,
    form, region,
    querySelector(selector) {
      if (selector === "form[data-preserve-refresh]") return form;
      if (selector === '[data-refresh-region="nodes-status"]') return region;
      return null;
    },
    replaceWith(next) { this.replaced = true; this.isConnected = false; document.root = next; next.isConnected = true; }
  };
  document.root = root;
  return root;
}

function environment(page, opts, fetchImpl) {
  let intervalActive = false;
  let lastController = null;
  const document = { root: null, activeElement: null, visibilityState: "visible", listeners: {},
    querySelector(selector) { return selector === "[data-admin-page]" ? this.root : null; },
    addEventListener(name, fn) { this.listeners[name] = fn; } };
  const window = { location: { href: (opts && opts.path) ? "http://hub" + opts.path : "http://hub/admin/" + page, pathname: (opts && opts.path) ? opts.path : "/admin/" + page }, listeners: {},
    addEventListener(name, fn) { this.listeners[name] = fn; } };
  const root = rootFor(page, document, opts);
  const next = rootFor(page, document, opts);
  next.isConnected = false;
  document.root = root;
  const context = {
    document, window, Error, DOMParser: function() { this.parseFromString = () => ({ querySelector: () => next }); },
    fetch: fetchImpl, AbortController: function() { lastController = this; this.signal = {}; this.abort = () => { this.aborted = true; }; },
    setInterval: () => { intervalActive = true; return 1; }, clearInterval: () => { intervalActive = false; },
    setTimeout: () => 1, clearTimeout: () => {}
  };
  return { context, document, window, root, next, intervalActive: () => intervalActive, aborted: () => !!(lastController && lastController.aborted) };
}

async function settle() { for (let i = 0; i < 8; i++) await Promise.resolve(); }

(async () => {
  let calls = 0;
  let env = environment("overview", { seconds: 8 }, () => { calls++; return Promise.resolve({ ok: true, text: () => Promise.resolve("<html>") }); });
  vm.runInNewContext(product, env.context);
  await settle();
  assert.strictEqual(env.window.DevBoardAdminRefresh.intervalMs, 8000);
  assert.strictEqual(calls, 1);
  assert.strictEqual(env.root.replaced, true);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.timerActive, true);

  env.document.visibilityState = "hidden";
  env.document.listeners.visibilitychange();
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.hidden, true);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.timerActive, false);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.stopped, false);
  const beforeVisible = env.window.DevBoardAdminRefresh.state.refreshCount;
  env.document.visibilityState = "visible";
  env.document.listeners.visibilitychange();
  await settle();
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.hidden, false);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.timerActive, true);
  assert.ok(env.window.DevBoardAdminRefresh.state.refreshCount > beforeVisible);

  let releaseHidden;
  env = environment("overview", { seconds: 8 }, () => new Promise(resolve => { releaseHidden = resolve; }));
  vm.runInNewContext(product, env.context);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.inFlight, true);
  env.document.visibilityState = "hidden";
  env.document.listeners.visibilitychange();
  assert.strictEqual(env.aborted(), true);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.timerActive, false);
  releaseHidden({ ok: true, text: () => Promise.resolve("<html>") });
  await settle();
  env.document.visibilityState = "visible";
  env.document.listeners.visibilitychange();
  await settle();
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.hidden, false);
  assert.ok(env.window.DevBoardAdminRefresh.state.refreshCount >= 1);

  calls = 0;
  env = environment("console", { seconds: 8, path: "/admin/settings" }, () => { calls++; throw new Error("settings refreshed"); });
  vm.runInNewContext(product, env.context);
  await settle();
  assert.strictEqual(calls, 0);
  assert.strictEqual(env.window.DevBoardAdminRefresh.state.enabled, false);

  calls = 0;
  env = environment("nodes", { seconds: 8 }, () => { calls++; return Promise.resolve({ ok: true, text: () => Promise.resolve("<html>") }); });
  env.document.activeElement = env.root.form;
  env.root.form.dataset.dirty = "true";
  vm.runInNewContext(product, env.context);
  await settle();
  assert.strictEqual(calls, 0);
  assert.strictEqual(env.root.replaced, false);
  assert.strictEqual(env.root.region.replaced, false);

  let release;
  env = environment("overview", { disabled: true }, () => Promise.resolve({ ok: true, text: () => Promise.resolve("<html>") }));
  vm.runInNewContext(product, env.context);
  const controller = env.window.DevBoardAdminRefresh;
  let pending = new Promise(resolve => { release = resolve; });
  let overlapCalls = 0;
  env.context.fetch = () => { overlapCalls++; return pending; };
  controller.state.enabled = true;
  const first = controller.refresh();
  const second = await controller.refresh();
  assert.strictEqual(second, false);
  release({ ok: true, text: () => Promise.resolve("<html>") });
  await first;
  assert.strictEqual(overlapCalls, 1);
  process.stdout.write("admin refresh behavior passed\n");
})().catch(err => { console.error(err); process.exit(1); });
`
	script := "const product = " + string(encoded) + ";\n" + harness
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("admin refresh behavior: %v: %s", err, strings.TrimSpace(string(out)))
	}
}
