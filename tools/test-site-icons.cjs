const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../internal/pages/home/site_icons.go"), "utf8");
const script = source.slice(source.indexOf("`") + 1, source.lastIndexOf("`"));

function icon(host, options = {}) {
  return {
    tagName: "IMG",
    isConnected: true,
    top: options.top ?? 100,
    details: options.details,
    modal: options.modal,
    dataset: { siteIconSrc: `/assets/site-icons?src=${encodeURIComponent(`https://${host}/favicon.ico`)}` },
    hasAttribute(name) { return name === "data-site-icon-src"; },
    closest(selector) {
      if (selector === "details:not([open])") return this.details && !this.details.open ? this.details : null;
      if (selector === ".application-subdirectory-modal") return this.modal || null;
      return null;
    },
    getBoundingClientRect() {
      return { width: 24, height: 24, top: this.top, bottom: this.top + 24, left: 100, right: 124 };
    }
  };
}

function runtime(nodes, { observe = true, complete = false } = {}) {
  const listeners = { window: new Map(), document: new Map() };
  const timers = [];
  const frames = [];
  const requests = [];
  let observer;
  function listen(surface, name, callback) {
    const callbacks = listeners[surface].get(name) || [];
    callbacks.push(callback);
    listeners[surface].set(name, callbacks);
  }
  function dispatch(surface, name, event = {}) {
    for (const callback of listeners[surface].get(name) || []) callback(event);
  }
  const fetch = (url, options) => {
    requests.push({ url, options });
    return new Promise(() => {});
  };
  const window = {
    fetch, URL, AbortController, ReadableStream, TextDecoder,
    innerHeight: 800, innerWidth: 1200,
    location: { href: "http://localhost/", origin: "http://localhost", hash: "" },
    addEventListener: (name, callback) => listen("window", name, callback),
    setTimeout: (callback) => timers.push(callback),
    requestAnimationFrame: (callback) => frames.push(callback)
  };
  if (observe) {
    window.IntersectionObserver = class {
      constructor(callback) { observer = this; this.callback = callback; this.nodes = new Set(); }
      observe(node) { this.nodes.add(node); }
      unobserve(node) { this.nodes.delete(node); }
      disconnect() { this.disconnected = true; this.nodes.clear(); }
      intersect(targets) { this.callback(targets.map((target) => ({ target, isIntersecting: true }))); }
    };
  }
  const document = {
    readyState: complete ? "complete" : "loading",
    querySelectorAll: (selector) => selector === "[data-site-icon-src]" ? nodes : [],
    addEventListener: (name, callback) => listen("document", name, callback)
  };
  vm.runInNewContext(script, { document, window, fetch, URL, Map, Promise });
  async function settle() {
    while (frames.length) frames.shift()();
    await Promise.resolve();
  }
  return {
    window, requests, dispatch, settle,
    get observer() { return observer; },
    start() {
      assert.equal(requests.length, 0);
      if (!complete) {
        assert.equal(timers.length, 0);
        dispatch("window", "load");
      }
      assert.equal(requests.length, 0);
      assert.equal(timers.length, 1);
      timers.shift()();
    },
    sources() {
      return requests.flatMap((request) => JSON.parse(request.options.body).sources);
    }
  };
}

test("icons wait for visibility and open directories, deduplicate, and abort on navigation", async () => {
  const details = { open: false };
  const modal = { id: "application-subdir-modal-0" };
  const visible = icon("visible.example");
  const duplicate = icon("visible.example", { details });
  const folded = icon("folded.example", { details });
  const popup = icon("popup.example", { modal });
  const below = icon("below.example", { top: 2000 });
  const run = runtime([visible, duplicate, folded, popup, below]);
  run.start();
  assert.equal(run.requests.length, 0);
  run.observer.intersect([visible, duplicate, folded, popup]);
  await run.settle();
  assert.deepEqual(run.sources(), ["https://visible.example/favicon.ico"]);
  details.open = true;
  run.dispatch("document", "toggle");
  await run.settle();
  assert.deepEqual(run.sources(), ["https://visible.example/favicon.ico", "https://folded.example/favicon.ico"]);
  run.window.location.hash = `#${modal.id}`;
  run.dispatch("window", "hashchange");
  await run.settle();
  assert.equal(run.sources().at(-1), "https://popup.example/favicon.ico");
  run.observer.intersect([below]);
  await run.settle();
  assert.equal(run.sources().at(-1), "https://below.example/favicon.ico");
  assert.equal(run.sources().filter((source) => source.includes("visible.example")).length, 1);
  run.dispatch("window", "pagehide", { persisted: true });
  assert.equal(run.requests[0].options.signal.aborted, false);
  run.dispatch("window", "pagehide", { persisted: false });
  assert.equal(run.requests[0].options.signal.aborted, true);
  assert.equal(run.observer.disconnected, true);
});

test("already-loaded pages without IntersectionObserver scan only nearby open icons", async () => {
  const details = { open: false };
  const visible = icon("visible.example");
  const below = icon("below.example", { top: 2000 });
  const folded = icon("folded.example", { details });
  const run = runtime([visible, below, folded], { observe: false, complete: true });
  run.start();
  await run.settle();
  assert.deepEqual(run.sources(), ["https://visible.example/favicon.ico"]);
  below.top = 300;
  run.dispatch("window", "scroll");
  await run.settle();
  assert.equal(run.sources().at(-1), "https://below.example/favicon.ico");
  details.open = true;
  run.dispatch("document", "toggle");
  await run.settle();
  assert.equal(run.sources().at(-1), "https://folded.example/favicon.ico");
});
