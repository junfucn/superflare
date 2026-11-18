"use strict";
var labels = config.locale === "en" ? {
  checking: "Checking LAN", verified: "LAN confirmed", unknown: "LAN not confirmed",
  mismatch: "Different SuperFlare instance",
  unconfigured: "LAN origin not configured", offline: "Offline",
  local: "LAN selected", source: "Source selected"
} : {
  checking: "\u6b63\u5728\u8bc6\u522b\u5185\u7f51", verified: "\u5185\u7f51\u5df2\u786e\u8ba4", unknown: "\u5185\u7f51\u672a\u786e\u8ba4",
  mismatch: "SuperFlare \u5b9e\u4f8b\u4e0d\u5339\u914d",
  unconfigured: "\u672a\u914d\u7f6e\u5185\u7f51\u5165\u53e3", offline: "\u7f51\u7edc\u5df2\u65ad\u5f00",
  local: "\u5df2\u9009\u62e9\u5185\u7f51", source: "\u5df2\u9009\u62e9\u6e90\u5730\u5740"
};
var modeKey = "superflare.network.mode";
var cacheKey = "superflare.network.check:" + config.id + ":" + config.url;
var mode = "auto";
var state = "unknown";
var checkedAt = 0;
var generation = 0;
var pending = null;
var controller = null;
var expiryTimer = null;
var navigating = false;
var cancelled = false;
var redirectDeadline = Date.now() + 1200;
var links = Array.prototype.slice.call(document.querySelectorAll("a[data-network-local]"));
var controls = document.getElementById("local-network-controls");
var status = document.getElementById("local-network-status");
var modeSelect = document.getElementById("settings-local-network-mode");
try { mode = localStorage.getItem(modeKey) || "auto"; } catch (e) {}
if (["auto", "local", "source"].indexOf(mode) < 0) { mode = "auto"; }

function fresh() {
  if (navigator.onLine === false && state === "verified") { return false; }
  var age = Date.now() - checkedAt;
  return age >= 0 && age < (state === "verified" ? 30000 : 5000);
}
function target(link) {
  var local = mode === "local" || (mode === "auto" && state === "verified" && fresh());
  return link.getAttribute(local ? "data-network-local" : "data-network-source");
}
function render() {
  links.forEach(function(link) {
    // Leave a real href for middle-click, modifier-click and browser menus.
    var probeHref = mode === "auto" && !fresh() && link.getAttribute("data-network-redirect");
    link.href = probeHref || target(link);
  });
  if (modeSelect) { modeSelect.value = mode; }
  var displayState = state === "verified" && !fresh() ? "unknown" : state;
  if (status) { status.textContent = labels[mode === "auto" ? displayState : mode] || labels.unknown; }
  if (controls) { controls.setAttribute("data-state", state); }
}
function invalidate() {
  generation++;
  if (controller) { controller.abort(); controller = null; }
  pending = null;
  checkedAt = 0;
  state = "unknown";
  window.clearTimeout(expiryTimer);
  try { sessionStorage.removeItem(cacheKey); } catch (e) {}
  render();
}
function remember(next) {
  state = next;
  checkedAt = Date.now();
  try { sessionStorage.setItem(cacheKey, JSON.stringify({ state: state, time: checkedAt })); } catch (e) {}
  window.clearTimeout(expiryTimer);
  expiryTimer = window.setTimeout(function() { render(); }, state === "verified" ? 30001 : 5001);
  render();
  return state;
}
function check() {
  if (fresh()) { return Promise.resolve(state); }
  if (pending) { return pending; }
  if (!config.url || !config.id) { return Promise.resolve(remember("unconfigured")); }
  if (navigator.onLine === false) { return Promise.resolve(remember("offline")); }
  var endpoint;
  try { endpoint = new URL(config.url); } catch (e) { return Promise.resolve(remember("unconfigured")); }
  if (!window.fetch || !window.AbortController) { return Promise.resolve(remember("unknown")); }
  var budget = config.redirect ? Math.max(0, redirectDeadline - Date.now()) : 1200;
  if (!budget) { return Promise.resolve(remember("unknown")); }
  var version = generation;
  var requestController = new AbortController();
  controller = requestController;
  state = "checking";
  render();
  var timeout;
  var expired = new Promise(function(resolve) {
    timeout = window.setTimeout(function() { requestController.abort(); resolve("unknown"); }, budget);
  });
  var request = Promise.resolve().then(function() {
    var options = {
      method: "GET", mode: "cors", cache: "no-store", credentials: "omit",
      redirect: "error", referrerPolicy: "no-referrer", signal: requestController.signal
    };
    if (window.location.protocol === "https:" && endpoint.protocol === "http:") {
      // Supporting browsers can prompt for LAN access instead of blocking mixed content outright.
      var host = endpoint.hostname.replace(/\.$/, "");
      var loopback = host === "localhost" || host.endsWith(".localhost") ||
        /^127\.\d+\.\d+\.\d+$/.test(host) || host === "[::1]";
      options.targetAddressSpace = loopback ? "loopback" : "local";
    }
    return fetch(endpoint.href, options);
  }).then(function(response) {
    if (!response.ok || !(response.headers.get("content-type") || "").includes("application/json")) { return "unknown"; }
    return response.json().then(function(value) {
      return value && value.service === "superflare" && value.id === config.id ? "verified" : "mismatch";
    });
  }).catch(function() { return "unknown"; });
  pending = Promise.race([request, expired]).then(function(result) {
    window.clearTimeout(timeout);
    if (version !== generation) { return state; }
    pending = null;
    controller = null;
    return remember(result);
  });
  return pending;
}
function setMode(next) {
  mode = next;
  try { localStorage.setItem(modeKey, mode); } catch (e) {}
  invalidate();
  if (mode === "auto") { check(); }
}
if (modeSelect) { modeSelect.addEventListener("change", function() { setMode(modeSelect.value); }); }
var retry = document.getElementById("local-network-retry");
if (retry) { retry.addEventListener("click", function() { invalidate(); check(); }); }

function refresh() {
  if (document.visibilityState === "hidden") { return; }
  invalidate();
  if (mode === "auto") { check(); }
}
window.addEventListener("focus", refresh);
window.addEventListener("online", refresh);
window.addEventListener("offline", function() { invalidate(); remember("offline"); });
window.addEventListener("pageshow", function(event) { if (event.persisted) { refresh(); } });
document.addEventListener("visibilitychange", function() {
  if (document.visibilityState === "hidden") { invalidate(); } else { refresh(); }
});
if (navigator.connection && navigator.connection.addEventListener) { navigator.connection.addEventListener("change", refresh); }
window.addEventListener("storage", function(event) {
  if (event.key === modeKey) {
    mode = ["auto", "local", "source"].indexOf(event.newValue) >= 0 ? event.newValue : "auto";
    refresh();
  }
});
links.forEach(function(link) {
  function prepare() { render(); if (mode === "auto" && !fresh()) { check(); } }
  link.addEventListener("pointerdown", prepare);
  link.addEventListener("focus", prepare);
  link.addEventListener("click", function() { render(); });
  link.addEventListener("auxclick", function() { render(); });
});

try {
  var cached = JSON.parse(sessionStorage.getItem(cacheKey));
  if (cached && ["verified", "unknown", "mismatch", "unconfigured", "offline"].indexOf(cached.state) >= 0 && typeof cached.time === "number") {
    state = cached.state; checkedAt = cached.time;
  }
} catch (e) {}
if (!fresh()) { state = "unknown"; checkedAt = 0; }
else { expiryTimer = window.setTimeout(render, Math.max(1, (state === "verified" ? 30000 : 5000) - (Date.now() - checkedAt) + 1)); }
render();

if (config.redirect) {
  document.querySelectorAll("[data-network-manual]").forEach(function(link) {
    link.addEventListener("click", function() { cancelled = true; invalidate(); });
  });
  function navigate() {
    if (cancelled || navigating) { return; }
    var version = generation;
    var choose = mode === "auto" ? check() : Promise.resolve(state);
    choose.then(function() {
      if (cancelled || navigating) { return; }
      // Focus and network events may have replaced the probe while awaiting it.
      if (version !== generation) { navigate(); return; }
      navigating = true;
      window.location.replace(mode === "local" || (mode === "auto" && state === "verified" && fresh()) ? config.local : config.source);
    });
  }
  navigate();
} else if (mode === "auto" && document.visibilityState !== "hidden") {
  check();
}
