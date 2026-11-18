const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const script = fs.readFileSync(path.join(__dirname, '../internal/localnetwork/client.js'), 'utf8');
const settle = () => new Promise(resolve => setImmediate(resolve));

function runtime({ mode = 'auto', url = 'https://nav.home.arpa/api/local-network/identity', redirect = false, cache, storageBlocked = false } = {}) {
  let now = 100000;
  let timerID = 0;
  const timers = new Map();
  const requests = [];
  const navigations = [];
  const windowEvents = {};
  const documentEvents = {};
  const values = new Map();
  if (cache) values.set('superflare.network.check:instance:' + url, JSON.stringify({ state: 'verified', time: now }));
  const storage = {
    getItem(key) { if (storageBlocked) throw Error('blocked'); return key === 'superflare.network.mode' ? mode : values.get(key) || null; },
    setItem(key, value) { if (storageBlocked) throw Error('blocked'); values.set(key, value); },
    removeItem(key) { if (storageBlocked) throw Error('blocked'); values.delete(key); }
  };
  function node(attrs = {}) {
    return {
      attrs, events: {}, href: '/redir/local', textContent: '',
      setAttribute(name, value) { this.attrs[name] = value; },
      getAttribute(name) { return this.attrs[name]; },
      addEventListener(name, callback) { this.events[name] = callback; },
      click() { this.events.click?.({}); }, focus() {}
    };
  }
  const links = Array.from({ length: 4 }, (_, n) => node({
    'data-network-local': 'http://192.168.2.10/service' + n,
    'data-network-source': 'https://service.example.com/' + n,
    'data-network-redirect': '/redir/local?item=' + n
  }));
  const modeSelect = node();
  const manual = node();
  const status = node();
  const controls = node();
  const retry = node();
  const document = {
    visibilityState: 'visible',
    querySelectorAll(selector) {
      if (selector === 'a[data-network-local]') return links;
      if (selector === '[data-network-manual]') return [manual];
      return [];
    },
    getElementById(id) { return { 'settings-local-network-mode': modeSelect, 'local-network-status': status, 'local-network-controls': controls, 'local-network-retry': retry }[id]; },
    addEventListener(name, callback) { documentEvents[name] = callback; }
  };
  const fetch = (address, options) => new Promise(resolve => requests.push({ address, options, resolve }));
  const window = {
    fetch, AbortController,
    location: { protocol: 'https:', replace(url) { navigations.push(url); } },
    setTimeout(callback, delay) { const id = ++timerID; timers.set(id, { callback, when: now + delay }); return id; },
    clearTimeout(id) { timers.delete(id); },
    addEventListener(name, callback) { windowEvents[name] = callback; }
  };
  vm.runInNewContext(script, {
    config: { id: 'instance', url, locale: 'en', redirect, local: 'http://nas/', source: 'https://nas.example.com/' },
    document, window, navigator: { onLine: true }, localStorage: storage, sessionStorage: storage,
    fetch, URL, AbortController, Promise, Date: { now: () => now }
  });
  return {
    links, modeSelect, status, controls, retry, requests, navigations, manual, document,
    fire(name, event = {}) { windowEvents[name]?.(event); },
    documentEvent(name) { documentEvents[name]?.(); },
    async respond(index = 0, value = { service: 'superflare', id: 'instance' }, ok = true) {
      requests[index].resolve({ ok, headers: { get: () => 'application/json' }, json: async () => value });
      await settle();
    },
    async advance(ms) {
      now += ms;
      for (const [id, timer] of [...timers]) if (timer.when <= now) { timers.delete(id); timer.callback(); }
      await settle();
    }
  };
}

test('one credential-free identity probe updates every bookmark without probing services', async () => {
  const page = runtime();
  await settle();
  assert.equal(page.requests.length, 1);
  assert.equal(page.requests[0].address, 'https://nav.home.arpa/api/local-network/identity');
  assert.equal(page.requests[0].options.credentials, 'omit');
  assert.equal(page.requests[0].options.redirect, 'error');
  assert.equal(page.requests[0].options.mode, 'cors');
  await page.respond();
  for (const link of page.links) assert.equal(link.href, link.attrs['data-network-local']);
  assert.equal(page.status.textContent, 'LAN confirmed');
});

test('a different instance or an error response falls back immediately', async () => {
  for (const [value, ok] of [[{ service: 'superflare', id: 'another' }, true], [{ service: 'superflare', id: 'instance' }, false]]) {
    const page = runtime({ redirect: true });
    await settle();
    await page.respond(0, value, ok);
    assert.deepEqual(page.navigations, ['https://nas.example.com/']);
  }
});

test('timeout aborts and late success cannot overwrite fallback', async () => {
  const page = runtime({ redirect: true });
  await settle();
  await page.advance(1200);
  assert.equal(page.requests[0].options.signal.aborted, true);
  assert.deepEqual(page.navigations, ['https://nas.example.com/']);
  await page.respond();
  assert.deepEqual(page.navigations, ['https://nas.example.com/']);
  assert.notEqual(page.controls.attrs['data-state'], 'verified');
});

test('positive cache expires and subsequent activation preserves a detection href', async () => {
  const page = runtime({ cache: true });
  await settle();
  assert.equal(page.requests.length, 0);
  assert.equal(page.links[0].href, page.links[0].attrs['data-network-local']);
  await page.advance(30001);
  page.links[0].events.click();
  assert.equal(page.links[0].href, page.links[0].attrs['data-network-redirect']);
});

test('network change invalidates positive cache and ignores old in-flight results', async () => {
  const page = runtime();
  await settle();
  page.fire('online');
  await settle();
  assert.equal(page.requests.length, 2);
  await page.respond(0);
  assert.notEqual(page.controls.attrs['data-state'], 'verified');
  await page.respond(1, { service: 'other', id: 'instance' });
  assert.equal(page.links[0].href, page.links[0].attrs['data-network-source']);
  page.fire('pageshow', { persisted: true });
  await settle();
  assert.equal(page.requests.length, 3);
});

test('manual modes bypass probes and survive unavailable browser storage', async () => {
  for (const mode of ['local', 'source']) {
    const page = runtime({ mode, redirect: true });
    await settle();
    assert.equal(page.requests.length, 0);
    assert.equal(page.navigations[0], mode === 'local' ? 'http://nas/' : 'https://nas.example.com/');
  }
  const page = runtime({ storageBlocked: true });
  page.modeSelect.value = 'local';
  page.modeSelect.events.change();
  assert.equal(page.links[0].href, page.links[0].attrs['data-network-local']);
});

test('mixed content and missing configuration are unconfirmed without service requests', async () => {
  for (const url of ['', 'http://192.168.1.10:3636/api/local-network/identity']) {
    const page = runtime({ url, redirect: true });
    await settle();
    assert.equal(page.requests.length, 0);
    assert.deepEqual(page.navigations, ['https://nas.example.com/']);
  }
});

test('manual navigation cancels any pending automatic navigation', async () => {
  const page = runtime({ redirect: true });
  await settle();
  page.manual.click();
  await page.respond();
  assert.deepEqual(page.navigations, []);
});

test('redirect waits for a probe restarted by focus without extending its deadline', async () => {
  const page = runtime({ redirect: true });
  await settle();
  await page.advance(800);
  page.fire('focus');
  await settle();
  await page.respond(0);
  assert.deepEqual(page.navigations, []);
  await page.advance(400);
  assert.deepEqual(page.navigations, ['https://nas.example.com/']);
  assert.equal(page.requests[1].options.signal.aborted, true);
});
