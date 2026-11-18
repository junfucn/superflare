const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const template = fs.readFileSync(path.join(__dirname, '../embed/templates/settings-ports.html'), 'utf8');
const script = template.slice(template.indexOf('(function () {'), template.indexOf('}());') + 5)
  .replace(/{{ T \.Locale "((?:port_|ports_)\w+)" }}/g, '$1')
  .replace(/{{[\s\S]*?}}/g, 'label');

function portPage() {
  const nodes = new Map();
  function node(id) {
    if (!nodes.has(id)) {
      nodes.set(id, {
        checked: false, disabled: false, textContent: id === 'data-ports' ? '[]' : '',
        action: '/settings/ports', events: {},
        addEventListener(event, handler) { this.events[event] = handler; },
        querySelector: () => node(id === 'form-port-collection' ? 'save-collection' : 'save'),
      });
    }
    return nodes.get(id);
  }
  let rows = [];
  let pending;
  let confirmRefresh = false;
  let confirmations = 0;
  const windowEvents = {};
  const requests = [];
  const table = {
    getSourceData: () => rows,
    getActiveEditor: () => null,
    loadData: (items) => { rows = items; },
    updateSettings() {},
  };
  const context = {
    document: { getElementById: node }, URLSearchParams,
    window: {
      confirm: () => { confirmations++; return confirmRefresh; },
      addEventListener: (name, handler) => { windowEvents[name] = handler; },
    },
    Handsontable: function () { return table; },
    fetch: (url, options = {}) => {
      requests.push({ url, ...options });
      return new Promise((resolve) => { pending = resolve; });
    },
  };
  vm.runInNewContext(script, context);
  return {
    node, table, requests, windowEvents,
    allowRefresh: () => { confirmRefresh = true; },
    confirmations: () => confirmations,
    showHidden: (checked) => {
      node('show-hidden-ports').checked = checked;
      node('show-hidden-ports').events.change();
    },
    respond: (payload, ok = true, redirected = false) => pending({
      ok, redirected, json: async () => payload && payload.ok === true ? { revision: 'revision-1', ...payload } : payload,
    }),
    submit: () => node('form-ports').events.submit({ preventDefault() {} }),
    submitCollection: () => {
      let prevented = false;
      node('form-port-collection').events.submit({ preventDefault() { prevented = true; } });
      return { prevented };
    },
  };
}

const settle = () => new Promise((resolve) => setImmediate(resolve));

test('port loading failure and malformed responses never enable empty saves', async () => {
  for (const [response, ok] of [[{ error: 'offline' }, false], [{ ok: true }, true], [{ ok: true, items: [], revision: '' }, true]]) {
    const page = portPage();
    assert.equal(page.node('save').disabled, true);
    page.submit();
    assert.equal(page.requests.length, 1);
    page.respond(response, ok);
    await settle();
    assert.equal(page.node('save').disabled, true);
    page.submit();
    assert.equal(page.requests.length, 1);
  }
});

test('port saves require a loaded snapshot and prevent overlapping requests', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Protocol: 'tcp', Remark: 'before', Running: true }] });
  await settle();
  assert.equal(page.node('save').disabled, false);
  page.table.getSourceData()[0].Remark = 'after';
  page.submit();
  const saved = new URLSearchParams(page.requests[1].body);
  assert.equal(JSON.parse(saved.get('ports'))[0].Remark, 'after');
  assert.equal(saved.get('includeHidden'), '1');
  assert.equal(saved.get('revision'), 'revision-1');
  page.submit();
  page.node('refresh-ports').events.click();
  assert.equal(page.requests.length, 2);
  assert.equal(page.node('save').disabled, true);
  assert.equal(page.submitCollection().prevented, true);
  page.respond({ ok: true, revision: 'revision-2' });
  await settle();
  assert.equal(page.requests.length, 3);
  assert.equal(page.node('save').disabled, true);
  assert.match(page.node('ports-status').textContent, /^ports_saved/);
  page.respond({ ok: true, items: [], revision: 'revision-2' });
  await settle();
  assert.equal(page.node('ports-status').textContent, 'ports_saved');
  assert.equal(page.node('save').disabled, false);
  page.submit();
  assert.equal(new URLSearchParams(page.requests[3].body).get('ports'), '[]');
});

test('port refresh failure retains save confirmation and login redirects get a readable error', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'before' }] });
  await settle();
  page.table.getSourceData()[0].Remark = 'saved';
  page.submit();
  page.respond({ ok: true, revision: 'revision-2' });
  await settle();
  page.respond(null, true, true);
  await settle();
  assert.equal(page.node('ports-status').textContent, 'ports_saved ports_load_failed: ports_session_expired');
  assert.equal(page.table.getSourceData()[0].Remark, 'saved');
  assert.equal(page.node('save').disabled, false);
  page.submit();
  assert.equal(new URLSearchParams(page.requests.at(-1).body).get('revision'), 'revision-2');
});

test('a version conflict retains drafts and the original revision until refresh', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'saved' }] });
  await settle();
  page.table.getSourceData()[0].Remark = 'second tab draft';
  page.submit();
  page.respond({ ok: false, error: 'version conflict' }, false);
  await settle();
  assert.match(page.node('ports-status').textContent, /version conflict/);
  assert.equal(page.table.getSourceData()[0].Remark, 'second tab draft');
  page.submit();
  assert.equal(new URLSearchParams(page.requests.at(-1).body).get('revision'), 'revision-1');
});

test('port drafts include open cells and remain protected when leaving or refreshing', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'saved' }] });
  await settle();
  let opened = true;
  page.table.getActiveEditor = () => ({
    isOpened: () => opened, getValue: () => 'open cell draft', originalValue: 'saved',
    finishEditing: () => { page.table.getSourceData()[0].Remark = 'open cell draft'; opened = false; },
  });
  const event = { preventDefault() { this.prevented = true; } };
  page.windowEvents.beforeunload(event);
  assert.equal(event.prevented, true);
  page.node('refresh-ports').events.click();
  assert.equal(page.confirmations(), 1);
  assert.equal(page.requests.length, 1);
  page.submit();
  assert.equal(JSON.parse(new URLSearchParams(page.requests.at(-1).body).get('ports'))[0].Remark, 'open cell draft');
});

test('port visibility filters preserve drafts for visible and hidden ports without fetching', async () => {
  const page = portPage();
  assert.match(page.requests[0].url, /\?includeHidden=1$/);
  page.respond({ ok: true, items: [
    { Port: 8080, Protocol: 'tcp', Remark: 'visible', Running: true },
    { Port: 9090, Protocol: 'tcp', Remark: 'hidden', Hidden: true },
  ] });
  await settle();
  assert.equal(page.table.getSourceData().length, 1);
  page.table.getSourceData()[0].Remark = 'visible draft';
  page.showHidden(true);
  assert.equal(page.table.getSourceData()[0].Remark, 'visible draft');
  page.table.getSourceData()[1].Remark = 'hidden draft';
  page.showHidden(false);
  page.showHidden(true);
  assert.equal(page.requests.length, 1);
  assert.equal(page.table.getSourceData()[1].Remark, 'hidden draft');
  page.showHidden(false);
  page.submit();
  const saved = new URLSearchParams(page.requests[1].body);
  assert.equal(saved.get('includeHidden'), '1');
  assert.deepEqual(JSON.parse(saved.get('ports')), [
    { Port: 8080, Protocol: 'tcp', Hidden: false, Remark: 'visible draft' },
    { Port: 9090, Protocol: 'tcp', Hidden: true, Remark: 'hidden draft' },
  ]);
  page.respond({ error: 'save failed' }, false);
  await settle();
  page.showHidden(true);
  assert.equal(page.table.getSourceData()[0].Remark, 'visible draft');
  assert.equal(page.table.getSourceData()[1].Remark, 'hidden draft');
  assert.equal(page.node('save').disabled, false);
});

test('refresh requires confirmation for drafts and a failed refresh keeps them usable', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'saved' }] });
  await settle();
  page.table.getSourceData()[0].Remark = 'draft';
  page.node('refresh-ports').events.click();
  assert.equal(page.requests.length, 1);
  assert.equal(page.confirmations(), 1);
  assert.equal(page.table.getSourceData()[0].Remark, 'draft');
  page.allowRefresh();
  page.node('refresh-ports').events.click();
  assert.equal(page.requests.length, 2);
  page.respond({ error: 'offline' }, false);
  await settle();
  assert.equal(page.table.getSourceData()[0].Remark, 'draft');
  assert.equal(page.node('save').disabled, false);
  page.node('refresh-ports').events.click();
  assert.equal(page.confirmations(), 3);
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'reloaded' }] });
  await settle();
  assert.equal(page.table.getSourceData()[0].Remark, 'reloaded');
});

test('unhide all includes ports excluded by the current visibility filter', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080 }, { Port: 9090, Hidden: true, Remark: 'kept' }] });
  await settle();
  page.node('unhide-all-ports').events.click();
  assert.equal(page.table.getSourceData().length, 2);
  assert(page.table.getSourceData().every((row) => row.Hidden === false));
  page.submit();
  assert.deepEqual(JSON.parse(new URLSearchParams(page.requests[1].body).get('ports')), [
    { Port: 9090, Protocol: 'tcp', Hidden: false, Remark: 'kept' },
  ]);
});

test('disabled collection shows saved rows as not collected and keeps remarks editable', async () => {
  const page = portPage();
  page.respond({ ok: true, collectionEnabled: false, items: [
    { Port: 8080, Protocol: 'tcp', Remark: 'saved', Running: false },
    { Port: 5353, Protocol: 'udp', Remark: '', Hidden: true },
  ], warnings: ['collection disabled'] });
  await settle();
  assert.equal(page.node('save').disabled, false);
  assert.equal(page.node('ports-status').textContent, 'collection disabled');
  assert.equal(page.table.getSourceData()[0].RunningText, 'port_not_collected');
  assert.equal(page.table.getSourceData()[0].PID, '');
  assert.equal(page.table.getSourceData()[0].ServiceName, '');
  page.table.getSourceData()[0].Remark = 'edited while disabled';
  page.submit();
  assert.deepEqual(JSON.parse(new URLSearchParams(page.requests[1].body).get('ports')), [
    { Port: 8080, Protocol: 'tcp', Hidden: false, Remark: 'edited while disabled' },
    { Port: 5353, Protocol: 'udp', Hidden: true, Remark: '' },
  ]);
});

test('saving the collection setting confirms before discarding unsaved remarks', async () => {
  const page = portPage();
  page.respond({ ok: true, items: [{ Port: 8080, Remark: 'saved' }] });
  await settle();
  assert.equal(page.submitCollection().prevented, false);
  assert.equal(page.confirmations(), 0);
  page.table.getSourceData()[0].Remark = 'draft';
  assert.equal(page.submitCollection().prevented, true);
  assert.equal(page.table.getSourceData()[0].Remark, 'draft');
  assert.equal(page.confirmations(), 1);
  page.allowRefresh();
  assert.equal(page.submitCollection().prevented, false);
  assert.equal(page.requests.length, 1);
});

test('the color picker consumes Escape but leaves Enter to the focused control', () => {
  const html = fs.readFileSync(path.join(__dirname, '../embed/templates/settings.html'), 'utf8');
  const start = html.indexOf('panel.addEventListener("keydown", function (event)');
  const end = html.indexOf('window.addEventListener("resize"', start);
  const globalScript = html.match(/<script>(document\.addEventListener\('keydown',[\s\S]*?)<\/script>/)[1];
  let panelKeydown;
  let documentKeydown;
  let restoreFocus = false;
  let navigations = 0;
  const context = {
    document: { addEventListener: (_, handler) => { documentKeydown = handler; } },
    panel: { addEventListener: (_, handler) => { panelKeydown = handler; } },
    hidePanel: (focus) => { restoreFocus = focus; },
    history: { back: () => { navigations++; } },
  };
  vm.runInNewContext(globalScript, context);
  vm.runInNewContext(html.slice(start, end), context);
  function dispatch(key) {
    const event = {
      key, defaultPrevented: false, stopped: false,
      target: { closest: () => null },
      preventDefault() { this.defaultPrevented = true; },
      stopPropagation() { this.stopped = true; },
    };
    panelKeydown(event);
    if (!event.stopped) documentKeydown(event);
    return event;
  }
  assert.equal(dispatch('Enter').defaultPrevented, false);
  assert.equal(restoreFocus, false);
  assert.equal(dispatch('Escape').defaultPrevented, true);
  assert.equal(restoreFocus, true);
  assert.equal(navigations, 0);
});
