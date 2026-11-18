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
    naturalWidth: options.broken ? 0 : 24,
    loading: "lazy",
    src: options.nativeSrc,
    currentSrc: options.nativeSrc,
    top: options.top ?? 100,
    details: options.details,
    modal: options.modal,
    dataset: options.direct ? { siteIconDirect: "1", siteIconFallbackSrc: "/assets/mdi/blackboard-bookmark.svg" } : options.nativeSrc ? (options.fallback ? { siteIconFallbackSrc: options.fallback } : {}) : { siteIconSrc: `/assets/site-icons?src=${encodeURIComponent(`https://${host}/favicon.ico`)}` },
    hasAttribute(name) {
      return (name === "data-site-icon-src" && !options.nativeSrc) ||
        (name === "data-site-icon-direct" && !!this.dataset.siteIconDirect) ||
        (name === "data-site-icon-fallback-src" && !!this.dataset.siteIconFallbackSrc);
    },
    getAttribute(name) { return name === "src" ? this.src : null; },
    removeAttribute(name) { if (name === "data-site-icon-direct") delete this.dataset.siteIconDirect; },
    addEventListener(name, callback) {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(callback);
    },
    removeEventListener(name, callback) { listeners.get(name)?.delete(callback); },
    finishLoad() {
      this.complete = true;
      for (const callback of [...(listeners.get("load") || [])]) callback();
    },
    failLoad() {
      this.complete = true;
      this.naturalWidth = 0;
      for (const callback of [...(listeners.get("error") || [])]) callback();
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

function runtime(nodes, { observe = true, complete = false, idle = true, deferDecode = false, resources = [] } = {}) {
  const listeners = { window: new Map(), document: new Map() };
  const timers = [];
  const frames = [];
  const requests = [];
  let imageScans = 0;
  const repairedSources = new Set();
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
    localStorage: {
      getItem(key) { return repairedSources.has(key) ? "1" : null; },
      setItem(key) { repairedSources.add(key); }
    },
    performance: { getEntriesByName(name) { return resources.filter((resource) => resource.name === name); } },
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
      if (selector === "img[data-site-icon-fallback-src]") return nodes.filter((node) => node.hasAttribute("data-site-icon-fallback-src"));
      if (selector === "img") { imageScans++; return nodes; }
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
    get imageScans() { return imageScans; },
    get repairedSources() { return repairedSources; },
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

test("explicit image icons fall back before and after script startup without favicon requests", async () => {
  const fallback = "/assets/mdi/blackboard-bookmark.svg";
  const broken = icon("broken.example", { nativeSrc: "https://broken.example/icon.png", fallback, broken: true });
  const pending = icon("pending.example", { nativeSrc: "https://pending.example/icon.png", fallback, pending: true });
  const healthy = icon("healthy.example", { nativeSrc: "https://healthy.example/icon.png", fallback });
  const run = runtime([broken, pending, healthy]);
  run.start();
  await run.settle();
  assert.equal(broken.src, fallback);
  assert.equal(pending.src, "https://pending.example/icon.png");
  pending.failLoad();
  assert.equal(pending.src, fallback);
  assert.equal(pending.dataset.siteIconFallbackDone, "1");
  assert.equal(healthy.src, "https://healthy.example/icon.png");
  pending.src = "replacement.svg";
  pending.failLoad();
  assert.equal(pending.src, "replacement.svg", "fallback must only run once");
  assert.equal(run.requests.length, 0);
});

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

test("IntersectionObserver handles scrolling without rescanning every image", async () => {
  const first = icon("first.example");
  const next = icon("next.example", { top: 2000 });
  const run = runtime([first, next]);
  run.start();
  const initialImageScans = run.imageScans;
  run.dispatch("window", "scroll");
  await run.settle();
  assert.equal(run.imageScans, initialImageScans);
  run.observer.intersect([next]);
  await run.settle();
  assert.equal(run.sources().at(-1), "https://next.example/favicon.ico");
});

test("fresh direct favicon loads skip the browser-cache repair request", async () => {
  const src = "http://localhost/assets/site-icons?src=https%3A%2F%2Fcached.example%2Ffavicon.ico";
  const direct = icon("cached.example", { direct: true, nativeSrc: src });
  const run = runtime([direct], { resources: [{ name: src, transferSize: 312 }] });
  run.start();
  await run.settle();
  assert.equal(run.requests.length, 0);
  assert.equal(run.repairedSources.has(`superflare.site-icon.primary-favicon:${src}`), true);
});

test("cached direct favicon loads still receive the one-time repair request", async () => {
  const src = "http://localhost/assets/site-icons?src=https%3A%2F%2Fcached.example%2Ffavicon.ico";
  const direct = icon("cached.example", { direct: true, nativeSrc: src });
  const run = runtime([direct], { resources: [{ name: src, transferSize: 0 }] });
  run.start();
  await run.settle();
  assert.equal(run.requests.length, 1);
  assert.equal(run.requests[0].url, src);
});

test("pending direct favicons finish their native load before deciding to repair", async () => {
  const src = "http://localhost/assets/site-icons?src=https%3A%2F%2Fpending.example%2Ffavicon.ico";
  for (const transferSize of [312, 0]) {
    const direct = icon("pending.example", { direct: true, nativeSrc: src, pending: true });
    const resources = [];
    const run = runtime([direct], { resources });
    run.start();
    await run.settle();
    assert.equal(run.requests.length, 0, "a pending native load must not race a second request");
    resources.push({ name: src, transferSize });
    direct.finishLoad();
    await run.settle();
    assert.equal(run.requests.length, transferSize ? 0 : 1);
    if (!transferSize) assert.equal(run.requests[0].url, src);
  }
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
  const children = Array.from({ length: 10 }, (_, i) => icon(`child-${i}.example`, { modal }));
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
  assert.deepEqual(JSON.parse(run.requests[1].options.body).sources, children.slice(0, 8).map((_, i) => `https://child-${i}.example/favicon.ico`));
  assert.equal(run.requests[1].options.priority, "low");
  await run.idle();
  assert.equal(run.requests.length, 2, "only one background batch may be active");
  await run.completeRequest(1);
  await run.decodeAll();
  await run.idle();
  assert.deepEqual(JSON.parse(run.requests[2].options.body).sources, [
    "https://child-8.example/favicon.ico", "https://child-9.example/favicon.ico"
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
  const run = runtime([primary, child], { idle: false });
  run.start();
  await run.settle();
  run.observer.intersect([primary]);
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
