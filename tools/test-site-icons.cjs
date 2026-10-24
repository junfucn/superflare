const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../internal/pages/home/site_icons.go"), "utf8");
const script = source.slice(source.indexOf("`") + 1, source.lastIndexOf("`"));

function icon(host, options = {}) {
  const listeners = new Map();
  return {
    tagName: "IMG",
    isConnected: true,
    complete: !options.pending,
    loading: "lazy",
    src: options.nativeSrc,
    top: options.top ?? 100,
    details: options.details,
    modal: options.modal,
    dataset: options.nativeSrc ? {} : { siteIconSrc: `/assets/site-icons?src=${encodeURIComponent(`https://${host}/favicon.ico`)}` },
    hasAttribute(name) { return name === "data-site-icon-src" && !options.nativeSrc; },
    addEventListener(name, callback) {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(callback);
    },
    removeEventListener(name, callback) { listeners.get(name)?.delete(callback); },
    finishLoad() {
      this.complete = true;
      for (const callback of [...(listeners.get("load") || [])]) callback();
    },
    closest(selector) {
      if (selector === "details") return this.details || null;
      if (selector === "details:not([open])") return this.details && !this.details.open ? this.details : null;
      if (selector === ".application-subdirectory-modal") return this.modal || null;
      return null;
    },
    getBoundingClientRect() {
      return { width: 24, height: 24, top: this.top, bottom: this.top + 24, left: 100, right: 124 };
    }
  };
}

function runtime(nodes, { observe = true, complete = false, idle = true, deferDecode = false } = {}) {
  const listeners = { window: new Map(), document: new Map() };
  const timers = [];
  const frames = [];
  const requests = [];
  const idleCallbacks = [];
  const decodes = [];
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
    return new Promise((resolve) => requests.push({ url, options, resolve }));
  };
  const window = {
    fetch, URL, AbortController, ReadableStream, TextDecoder,
    innerHeight: 800, innerWidth: 1200,
    location: { href: "http://localhost/", origin: "http://localhost", hash: "" },
    addEventListener: (name, callback) => listen("window", name, callback),
    setTimeout: (callback) => timers.push(callback),
    requestAnimationFrame: (callback) => frames.push(callback)
  };
  if (idle) window.requestIdleCallback = (callback) => idleCallbacks.push(callback);
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
    querySelectorAll: (selector) => {
      if (selector === "[data-site-icon-src]") return nodes.filter((node) => node.hasAttribute("data-site-icon-src"));
      if (selector === "img") return nodes;
      if (selector === ".application-subdirectory-modal img,details img") return nodes.filter((node) => node.modal || node.details);
      return [];
    },
    addEventListener: (name, callback) => listen("document", name, callback)
  };
  class ProbeImage {
    decode() { return deferDecode ? new Promise((resolve) => decodes.push(resolve)) : Promise.resolve(); }
  }
  vm.runInNewContext(script, {
    document, window, fetch, URL, Map, Set, Promise, Blob, Uint8Array, TextDecoder, atob,
    Image: ProbeImage
  });
  async function settle() {
    while (frames.length) frames.shift()();
    for (let i = 0; i < 20; i++) await Promise.resolve();
  }
  return {
    window, requests, dispatch, settle,
    get observer() { return observer; },
    async idle() {
      const callback = idleCallbacks.shift() || timers.shift();
      if (callback) callback();
      await settle();
    },
    async decodeAll() {
      while (decodes.length) decodes.shift()();
      await settle();
    },
    async completeRequest(index) {
      const request = requests[index];
      if (request.url === "/assets/site-icons/stream") {
        const records = JSON.parse(request.options.body).sources.map((source) => JSON.stringify({
          source, contentType: "image/svg+xml",
          data: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"></svg>').toString("base64")
        })).join("\n") + "\n";
        let read = false;
        request.resolve({
          ok: true,
          body: { getReader: () => ({
            read: async () => {
              if (read) return { done: true };
              read = true;
              return { value: new TextEncoder().encode(records), done: false };
            },
            releaseLock() {}
          }) }
        });
      } else {
        request.resolve({ ok: false });
      }
      await settle();
    },
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
      return requests.filter((request) => request.options.body).flatMap((request) => JSON.parse(request.options.body).sources);
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

test("closed submenu icons prefetch in low-priority batches after foreground decoding", async () => {
  const modal = { id: "application-subdir-modal-0" };
  const primary = icon("primary.example");
  const duplicate = icon("primary.example", { modal });
  const children = Array.from({ length: 6 }, (_, i) => icon(`child-${i}.example`, { modal }));
  const run = runtime([primary, duplicate, ...children], { deferDecode: true });
  run.start();
  await run.settle();
  assert.deepEqual(run.sources(), ["https://primary.example/favicon.ico"]);
  assert.equal(run.requests[0].options.priority, "auto");
  await run.idle();
  assert.equal(run.requests.length, 1, "prefetch must wait for the primary request");
  await run.completeRequest(0);
  await run.idle();
  assert.equal(run.requests.length, 1, "prefetch must also wait for image decoding");
  await run.decodeAll();
  assert.equal(duplicate.dataset.siteIconDone, "1", "a shared source also fills the hidden copy");
  await run.idle();
  assert.deepEqual(JSON.parse(run.requests[1].options.body).sources, children.slice(0, 4).map((_, i) => `https://child-${i}.example/favicon.ico`));
  assert.equal(run.requests[1].options.priority, "low");
  await run.idle();
  assert.equal(run.requests.length, 2, "only one background batch may be active");
  await run.completeRequest(1);
  await run.decodeAll();
  await run.idle();
  assert.deepEqual(JSON.parse(run.requests[2].options.body).sources, [
    "https://child-4.example/favicon.ico", "https://child-5.example/favicon.ico"
  ]);
  await run.completeRequest(2);
  await run.decodeAll();
  assert.ok(children.every((child) => child.dataset.siteIconDone === "1" && child.loading === "eager"));
  assert.equal(run.window.location.hash, "", "prefetch must not open the menu");
  assert.equal(run.sources().filter((source) => source.includes("primary.example")).length, 1);
});

test("native submenu images preload after primary images, with the idle timer fallback", async () => {
  const details = { open: false };
  const primary = icon("primary.example", { nativeSrc: "https://primary.example/icon.png", pending: true });
  const child = icon("child.example", { details, nativeSrc: "https://child.example/icon.png", pending: true });
  const run = runtime([primary, child], { idle: false, observe: false });
  run.start();
  await run.settle();
  await run.idle();
  assert.equal(child.loading, "lazy", "the primary image takes precedence");
  primary.finishLoad();
  await run.settle();
  await run.idle();
  assert.equal(child.loading, "eager");
  assert.equal(child.fetchPriority, "low");
  assert.equal(run.requests.length, 0, "native images do not need a favicon request");
  assert.equal(details.open, false);
});

test("opening a menu loads its icons immediately while background work waits", async () => {
  const modal = { id: "application-subdir-modal-0" };
  const primary = icon("primary.example");
  const child = icon("child.example", { modal });
  const run = runtime([primary, child]);
  run.start();
  await run.settle();
  run.window.location.hash = `#${modal.id}`;
  run.dispatch("window", "hashchange");
  await run.settle();
  assert.deepEqual(run.sources(), ["https://primary.example/favicon.ico", "https://child.example/favicon.ico"]);
  assert.equal(run.requests[1].options.priority, "auto");
  await run.completeRequest(0);
  await run.completeRequest(1);
  await run.idle();
  assert.equal(run.requests.length, 2, "prefetch must not reload an opened menu");
});
