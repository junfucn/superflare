const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const page = fs.readFileSync(path.join(__dirname, '../embed/templates/editor.html'), 'utf8');
const english = require('../internal/i18n/locales/en.json');

test('LAN URL Enter preserves selected cells and commits only an open empty editor', () => {
  const generatedPage = fs.readFileSync(path.join(__dirname, '../internal/resources/templates/html/editor.html'), 'utf8');
  for (const template of [page, generatedPage]) {
    const start = template.indexOf("instanceBookmarks.addHook('beforeKeyDown'");
    const end = template.indexOf('\n        });', start);
    assert(start >= 0 && end > start);
    const block = template.slice(start, end + '\n        });'.length);
    for (const scenario of [
      { opened: false, input: '' },
      { opened: true, input: 'http://192.168.1.2:8080' },
      { opened: true, input: '', dropdownSelected: true },
      { opened: true, input: '', prop: 'URL' },
      { noEditor: true },
      { opened: true, input: '', commits: true },
      { opened: true, input: '   ', commits: true },
    ]) {
      const original = 'http://192.168.1.1:8080';
      let value = original;
      let prevented = false;
      let stopped = false;
      let finished = 0;
      let handler;
      const editor = {
        TEXTAREA: { value: scenario.input },
        isOpened: () => scenario.opened === true,
        htEditor: { getSelectedLast: () => scenario.dropdownSelected ? [0, 0, 0, 0] : null },
        setValue(next) { this.TEXTAREA.value = next; },
        finishEditing(restore) {
          assert.equal(restore, false);
          value = this.TEXTAREA.value;
          scenario.opened = false;
          finished++;
        },
        close: () => assert.fail('finish the edit through the editor lifecycle'),
      };
      const table = {
        addHook(name, callback) { assert.equal(name, 'beforeKeyDown'); handler = callback; },
        getSelectedLast: () => [0, 2, 0, 2],
        colToProp: () => scenario.prop || 'LocalURL',
        getActiveEditor: () => scenario.noEditor ? null : editor,
        setDataAtRowProp: () => assert.fail('a key hook must not overwrite the selected cell directly'),
      };
      vm.runInNewContext(block, { instanceBookmarks: table });
      const event = {
        key: 'Enter',
        preventDefault() { prevented = true; },
        stopImmediatePropagation() { stopped = true; },
      };
      handler(event);
      assert.equal(value, scenario.commits ? '' : original);
      assert.equal(prevented, scenario.commits === true);
      assert.equal(stopped, scenario.commits === true);
      assert.equal(finished, scenario.commits ? 1 : 0);
      if (scenario.commits) {
        value = 'http://192.168.1.3:8080';
        handler(event);
        assert.equal(value, 'http://192.168.1.3:8080', 'a later Enter on the closed editor must not clear another value');
        assert.equal(finished, 1);
      }
    }
  }
});

test('editor preserves ungrouped bookmarks and their private/favorite state', () => {
  const start = page.indexOf('let categoriesMap = categories.reduce');
  const end = page.indexOf('function clampEditorColumnWidth', start);
  assert(start >= 0 && end > start);
  const context = {
    categories: [{ ID: ' 1 ', Name: ' Review ' }, { ID: '__proto__', Name: 'Special' }],
    dataBookmarks: [
      { Name: 'Ungrouped', URL: 'https://ungrouped.invalid/', Category: '', Favorite: true, Private: true },
      { Name: 'Grouped', URL: 'https://grouped.invalid/', Category: ' 1 ', Favorite: true, Private: true },
      { Name: 'App', URL: 'https://app.invalid/', IsApplication: true, Favorite: true },
      { Name: 'Special', URL: 'https://special.invalid/', Category: '__proto__' },
    ],
  };
  vm.runInNewContext(page.slice(start, end) + '\nglobalThis.result = bookmarks;', context);
  assert.equal(context.result.length, 4);
  const item = context.result.find((row) => row.Name === 'Ungrouped');
  assert.equal(item.Category, '[SuperFlare \u672a\u5206\u7c7b]');
  assert.equal(item.Favorite, true);
  assert.equal(item.Private, true);
  const grouped = context.result.find((row) => row.Name === 'Grouped');
  assert.equal(grouped.Category, 'Review');
  assert.equal(grouped.Favorite, true);
  assert.equal(grouped.Private, true);
  assert.equal(context.result.find((row) => row.Name === 'Special').Category, 'Special');
  assert.equal(context.result.find((row) => row.Name === 'App').Favorite, false);
});

test('editor distinguishes applications from categories matching the legacy app marker', () => {
  const generatedPage = fs.readFileSync(path.join(__dirname, '../internal/resources/templates/html/editor.html'), 'utf8');
  for (const template of [page, generatedPage]) {
    const start = template.indexOf('let categoriesMap = categories.reduce');
    const end = template.indexOf('function clampEditorColumnWidth', start);
    assert(start >= 0 && end > start);
    const context = {
      categories: [{ ID: '_FLARE_FIXED_CATEGORY', Name: 'Group' }],
      dataBookmarks: [
        { Name: 'App', URL: 'https://app.invalid/', Category: '_FLARE_FIXED_CATEGORY', IsApplication: true, Favorite: true },
        { Name: 'Bookmark', URL: 'https://bookmark.invalid/', Category: '_FLARE_FIXED_CATEGORY', IsApplication: false, Favorite: true, Private: true },
      ],
    };
    vm.runInNewContext(template.slice(start, end) + '\nglobalThis.result = bookmarks;', context);
    const app = context.result.find((row) => row.Name === 'App');
    assert.equal(app.Category, '[SuperFlare \u5e94\u7528]');
    assert.equal(app.Favorite, false);
    const bookmark = context.result.find((row) => row.Name === 'Bookmark');
    assert.equal(bookmark.Category, 'Group');
    assert.equal(bookmark.Favorite, true);
    assert.equal(bookmark.Private, true);
  }
});

test('save buttons serialize requests and keep newer drafts visible after success or failure', async () => {
  const buttons = ['Categories', 'Bookmarks'].map((label) => ({
    label, disabled: false,
    addEventListener(name, callback) { this[name] = callback; },
  }));
  const restore = { disabled: false };
  const form = { action: '/editor', entries: [] };
  const revisionField = { value: 'initial-revision' };
  const requests = [];
  const notices = [];
  const timers = new Map();
  const confirmations = [];
  const locations = [];
  let timerID = 0;
  let draft = 'Original';
  let acceptRestore = false;
  let activeEditor = null;
  const table = { getActiveEditor: () => activeEditor, getSettings: () => ({ readOnly: false }) };
  const context = {
    document: {
      getElementsByClassName: () => buttons,
      getElementById: (id) => id === 'field-revision' ? revisionField : form,
    },
    window: {
      setTimeout(callback) { timers.set(++timerID, callback); return timerID; },
      clearTimeout(id) { timers.delete(id); },
      addEventListener(name, callback) { this[name] = callback; },
      removeEventListener(name) { delete this[name]; },
      confirm(message) { confirmations.push(message); return acceptRestore; },
      location: { replace(path) { locations.push(path); } },
    },
    FormData: function (value) { return value.entries; },
    URLSearchParams,
    fetch: (url, options) => new Promise((resolve) => requests.push({ url, options, resolve })),
    editorMessages: {
      saveNewerChanges: 'Newer changes unsaved', saveSuccess: 'Saved', operationFailed: 'Failed',
      saveConflict: 'Data changed elsewhere. Preserve your draft before reloading.',
      restoreConfirm: 'Restore?', restoreConfirmDraft: 'Discard unsaved edits?', restoreFailedShort: 'Restore failed',
    },
    restoreSubmitButton: restore,
    restoreForm: { action: '/editor/restore', addEventListener(name, callback) { this[name] = callback; } },
    fileInput: { files: [{ name: 'backup.zip' }] },
    checkLinksButton: {},
    instanceCategories: table,
    instanceBookmarks: table,
    setBookmarkTableLinkCheckLocked() {},
    validateEditorDataBeforeSubmit: () => null,
    bindData: () => {
      form.entries = [['bookmarks', draft], ['revision', revisionField.value]];
      return JSON.stringify(['1,Group', draft]);
    },
    exportCategoriesCSV: () => '1,Group',
    exportBookmarksCSV: () => draft,
    getEditorButtonDefaultLabel: (button) => button.label,
    setButtonState: (button, state) => { button.state = state; },
    resetEditorButton() {},
    showEditorOperationNotice: (...args) => notices.push(args),
    formatEditorFailureMessage: (prefix, detail) => detail,
    parseEditorJSONFailure: async () => { throw new Error('Rejected'); },
  };
  let start = page.indexOf('async function requireEditorOperationSuccess');
  vm.runInNewContext(page.slice(start, page.indexOf('function summarizeEditorErrorMessage', start)), context);
  start = page.indexOf('const editorSaveButtons =');
  vm.runInNewContext(page.slice(start, page.indexOf("backupDownloadButton.addEventListener('click'", start)), context);
  const event = { preventDefault() {} };
  const waitForRequest = () => new Promise(setImmediate);
  const navigationBlocked = () => {
    let blocked = false;
    context.window.beforeunload({ preventDefault() { blocked = true; } });
    return blocked;
  };
  assert.equal(navigationBlocked(), false, 'an unchanged draft should not interrupt navigation');
  activeEditor = { isOpened: () => true, getValue: () => 'Uncommitted', originalValue: 'Original' };
  assert.equal(navigationBlocked(), true, 'typing in an open cell is an unsaved edit');
  activeEditor = null;
  const pending = buttons[0].click(event);
  assert(buttons.every((button) => button.disabled));
  assert.equal(restore.disabled, true);
  assert.equal(navigationBlocked(), true, 'a save in progress should interrupt navigation');
  await buttons[1].click(event);
  await waitForRequest();
  assert.equal(requests.length, 1, 'the second save must not overlap the pending request');
  let restorePrevented = false;
  context.restoreForm.submit({ preventDefault() { restorePrevented = true; } });
  assert.equal(restorePrevented, true);
  draft = 'Newer';
  assert.equal(new URLSearchParams(requests[0].options.body).get('bookmarks'), 'Original');
  assert.equal(new URLSearchParams(requests[0].options.body).get('revision'), 'initial-revision');
  const success = (revision = 'saved-revision') => new Response(JSON.stringify({ notice: 'save_success', revision }), { headers: { 'Content-Type': 'application/json' } });
  requests[0].resolve(success());
  await pending;
  assert.deepEqual(notices.at(-1), ['warning', 'Newer changes unsaved', false]);
  assert(buttons.every((button) => !button.disabled));
  assert.equal(restore.disabled, false);
  assert.equal(draft, 'Newer');
  assert.equal(revisionField.value, 'saved-revision', 'the next save must use the acknowledged server revision, even with newer local changes');

  const failed = buttons[1].click(event);
  assert.equal(timers.size, 0, 'old feedback must not reset a pending save');
  await waitForRequest();
  assert.equal(new URLSearchParams(requests[1].options.body).get('revision'), 'saved-revision');
  requests[1].resolve(new Response('{"error":"server conflict"}', { status: 409 }));
  await failed;
  assert.equal(notices.at(-1)[0], 'error');
  assert.equal(notices.at(-1)[1], context.editorMessages.saveConflict);
  assert(buttons.every((button) => !button.disabled));
  assert.equal(restore.disabled, false);
  assert.equal(draft, 'Newer');
  assert.equal(revisionField.value, 'saved-revision', 'a conflict must not rebase the stale draft onto the newer revision');
  assert.equal(navigationBlocked(), true, 'a failed save must leave the draft intact');

  activeEditor = {
    isOpened: () => true, getValue: () => 'Uncommitted', originalValue: 'Newer',
    finishEditing(restoreOriginal, ctrlDown, done) {
      assert.equal(restoreOriginal, false);
      draft = 'Uncommitted';
      activeEditor = null;
      done(true);
    },
  };
  const retry = buttons[0].click(event);
  await waitForRequest();
  assert.equal(new URLSearchParams(requests[2].options.body).get('bookmarks'), 'Uncommitted');
  requests[2].resolve(success('next-revision'));
  await retry;
  assert.deepEqual(notices.at(-1), ['success', 'Saved', true]);
  assert.equal(revisionField.value, 'next-revision');
  assert.equal(navigationBlocked(), false);

  draft = 'Local edit';
  await context.restoreForm.submit(event);
  assert.equal(confirmations.at(-1), 'Restore?\n\nDiscard unsaved edits?');
  assert.equal(requests.length, 3, 'cancelling restore should not start an upload');

  acceptRestore = true;
  const restoreFailure = context.restoreForm.submit(event);
  assert.equal(restore.disabled, true);
  assert(buttons.every((button) => button.disabled));
  await buttons[0].click(event);
  assert.equal(requests.length, 4, 'save must wait while restore is submitting');
  requests[3].resolve(new Response('{"error":"Invalid backup"}', { status: 400 }));
  await restoreFailure;
  assert.equal(restore.disabled, false);
  assert(buttons.every((button) => !button.disabled), 'restore failure must enable the controls');
  assert.equal(draft, 'Local edit');
  assert.equal(navigationBlocked(), true);
  assert.deepEqual(locations, []);

  const restoreSuccess = context.restoreForm.submit(event);
  requests[4].resolve(new Response('{"notice":"restore_success"}', { headers: { 'Content-Type': 'application/json' } }));
  await restoreSuccess;
  assert.deepEqual(locations, ['/editor?notice=restore_success']);
  assert.equal(context.window.beforeunload, undefined, 'the confirmed navigation should not prompt again');
});

test('editor only acknowledges an explicit JSON save confirmation', async () => {
  const start = page.indexOf('async function requireEditorOperationSuccess');
  const end = page.indexOf('function summarizeEditorErrorMessage', start);
  assert(start >= 0 && end > start);
  const context = {
    editorMessages: { operationFailed: 'Save failed' },
    parseEditorJSONFailure: async () => { throw new Error('Login expired'); },
  };
  vm.runInNewContext(page.slice(start, end), context);
  for (const response of [
    new Response('<html>Login</html>', { headers: { 'Content-Type': 'text/html' } }),
    new Response('{}', { headers: { 'Content-Type': 'application/json' } }),
    new Response('{"notice":"save_success"}', { headers: { 'Content-Type': 'application/json' } }),
    new Response('{"notice":"save_success","revision":""}', { headers: { 'Content-Type': 'application/json' } }),
    new Response('{', { headers: { 'Content-Type': 'application/json' } }),
    new Response('{}', { status: 401, headers: { 'Content-Type': 'application/json' } }),
    { ok: true, redirected: true, headers: new Headers({ 'Content-Type': 'application/json' }) },
  ]) {
    await assert.rejects(context.requireEditorSaveSuccess(response));
  }
  const payload = await context.requireEditorSaveSuccess(new Response('{"notice":"save_success","revision":"new-revision"}', {
    headers: { 'Content-Type': 'application/json; charset=UTF-8' },
  }));
  assert.equal(payload.notice, 'save_success');
});

test('backup rejects login pages, redirects and error responses without replacing the editor', async () => {
  const context = {
    editorMessages: { backupFailed: 'Backup failed' },
    document: {
      open: () => assert.fail('backup must preserve the editor document'),
      write: () => assert.fail('backup must preserve the editor document'),
    },
  };
  for (const [startText, endText] of [
    ['function parseEditorJSONFailure', 'async function requireEditorSaveSuccess'],
    ['function summarizeEditorErrorMessage', 'function validateEditorDataBeforeSubmit'],
  ]) {
    vm.runInNewContext(page.slice(page.indexOf(startText), page.indexOf(endText)), context);
  }
  for (const response of [
    new Response('<html>Login</html>', { headers: { 'Content-Type': 'text/html' } }),
    new Response('<html>Server error</html>', { status: 500, headers: { 'Content-Type': 'text/html' } }),
    new Response('{"error":"Login expired"}', { status: 401, headers: { 'Content-Type': 'application/json' } }),
    new Response('{}', { headers: { 'Content-Type': 'application/json' } }),
    new Response('', { headers: { 'Content-Type': 'application/zip' } }),
    { ok: true, redirected: true, headers: new Headers({ 'Content-Type': 'application/zip' }) },
  ]) {
    await assert.rejects(context.requireEditorBackupSuccess(response));
  }
  const zip = new Response(new Uint8Array([0x50, 0x4b, 3, 4]), { headers: { 'Content-Type': 'application/zip' } });
  const result = await context.requireEditorBackupSuccess(zip);
  assert.equal(result.response, zip);
  assert.equal(result.blob.size, 4);

  const button = {
    disabled: false,
    addEventListener: (name, handler) => { button.click = handler; },
  };
  const notices = [];
  const draft = [{ Name: 'Unsaved bookmark', Category: 'Unsaved category' }];
  Object.assign(context, {
    backupDownloadButton: button,
    bookmarks: draft,
    fetch: async (url, options) => {
      assert(options.headers.Accept.includes('application/json'));
      return new Response('<html>Login</html>', { headers: { 'Content-Type': 'text/html' } });
    },
    setButtonState() {},
    resetEditorButton() {},
    showEditorOperationNotice: (...args) => notices.push(args),
    formatEditorFailureMessage: (title, detail) => detail,
    window: { setTimeout: (callback) => callback() },
  });
  const start = page.indexOf("backupDownloadButton.addEventListener('click'");
  vm.runInNewContext(page.slice(start, page.indexOf('function buildDataTableColumns', start)), context);
  button.click.call(button, { preventDefault() {} });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(button.disabled, false);
  assert.equal(notices[0][0], 'error');
  assert.equal(context.bookmarks, draft);
});

test('add row remains available on empty tables and respects the link-check lock', () => {
  const context = { scheduleTableLayoutSync() {} };
  const start = page.indexOf('function appendEditorRow');
  vm.runInNewContext(page.slice(start, page.indexOf("addCategoryButton.addEventListener", start)), context);
  let rows = 0;
  let locked = false;
  const selections = [];
  const table = {
    countRows: () => rows,
    getSettings: () => ({ readOnly: locked }),
    alter: (action, row, count) => {
      assert.equal(action, 'insert_row');
      assert.equal(row, rows);
      rows += count;
    },
    selectCell: (row, col) => selections.push([row, col]),
    scrollViewportTo() {},
  };
  context.appendEditorRow(table);
  context.appendEditorRow(table);
  assert.equal(rows, 2);
  assert.deepEqual(selections, [[0, 0], [1, 0]]);
  rows = 0;
  context.appendEditorRow(table);
  assert.equal(rows, 1);
  locked = true;
  context.appendEditorRow(table);
  assert.equal(rows, 1);
});

function editorHarness(source, order, categoryNames = ['Group'], history = { doneActions: [], undoneActions: [] }) {
  const categoryRows = categoryNames.map((Name) => ({ Name }));
  const hooks = {};
  const notices = [];
  const table = {
    countRows: () => source.length,
    getDataAtRowProp: (row, prop) => source[order[row]][prop],
    toPhysicalRow: (row) => order[row],
    propToCol: (prop) => prop === 'Category' ? 3 : -1,
    getPlugin: (name) => name === 'undoRedo' ? history : null,
    setSourceDataAtCell: (changes) => {
      for (const [physicalRow, prop, value] of changes) source[physicalRow][prop] = value;
    },
    loadData: () => assert.fail('derived updates must not reset row mappings or undo history'),
    updateSettings() {},
    render: () => {},
    addHook: (name, fn) => {
      const previous = hooks[name];
      hooks[name] = (...args) => {
        if (previous && previous(...args) === false) return false;
        return fn(...args);
      };
    },
  };
  const context = {
    instanceBookmarks: table,
    instanceCategories: {
      countRows: () => categoryRows.length,
      getDataAtRowProp: (row, prop) => categoryRows[row][prop],
    },
    categories: [], categoriesMap: {},
    tableLocale: 'en-US',
    bookmarks: [],
    searchField: { value: '' },
    FLARE_FIX_CATEGORY: ['[SuperFlare \u5e94\u7528]'],
    FLARE_UNGROUPED_CATEGORY: '[SuperFlare \u672a\u5206\u7c7b]',
    localPortValueMap: {},
    buildDataTableColumns: () => [],
    showEditorOperationNotice: (type, text) => notices.push({ type, text }),
    rebuildBookmarkSearchIndex() {},
    runBookmarkSearch() {},
    scheduleTableLayoutSync() {},
    clearEditorValidationState() {},
    updateBookmarkSearchIndexRow() {},
    refreshBookmarkSearchAfterDataChange() {},
    addEditorValidationTarget() {},
  };
  const messageBlock = page.slice(page.indexOf('const editorMessages'), page.indexOf('const TABLE_MAX_COLUMN_WIDTH'));
  vm.runInNewContext(messageBlock
    .replace(/'{{ T \.Locale "([^"]+)" }}'/g, (_, key) => JSON.stringify(english[key]))
    .replace(/{{[\s\S]*?}}/g, ''), context);
  const blocks = [
    ['function csvEscape', 'function exportCategoriesCSV'],
    ['function normalizeEditorText', 'function clearEditorValidationState'],
    ['function getBookmarkVisualRowDataFromTable', 'function rebuildBookmarkSearchIndex'],
    ['function validateEditorDataBeforeSubmit', 'getEditorButtonDefaultLabel(backupDownloadButton);'],
    ['function summarizeLinkCheckReason', "checkLinksButton.addEventListener('click'"],
    ['function updateDataTable', 'function preserveEditorRowRemovalHistory'],
    ['function enforceApplicationFavoriteInvariant', "instanceBookmarks.addHook('afterCreateRow'"],
  ];
  for (const [startText, endText] of blocks) {
    const start = page.indexOf(startText);
    const end = page.indexOf(endText, start);
    assert(start >= 0 && end > start);
    vm.runInNewContext(page.slice(start, end), context);
  }
  const start = page.indexOf('beforeChange: function (changes)');
  const end = page.indexOf('afterCreateRow: function', start);
  assert(start >= 0 && end > start);
  vm.runInNewContext('globalThis.categoryHooks = {' + page.slice(start, end) + '};', context);
  return {
    context, hooks, notices,
    rename(changes) {
      if (context.categoryHooks.beforeChange(changes) === false) return false;
      for (const [row, prop, , value] of changes) categoryRows[row][prop] = value;
      context.categoryHooks.afterChange(changes, 'edit');
      return true;
    },
  };
}

test('new and blank-type bookmarks export as ungrouped without losing favorites', () => {
  const schema = page.match(/dataSchema: (\{ Name: '', URL: ''[^\n]+\}),/);
  assert(schema, 'bookmark schema must be available');
  const ungrouped = '[SuperFlare \u672a\u5206\u7c7b]';
  const bookmark = vm.runInNewContext('(' + schema[1] + ')', { FLARE_UNGROUPED_CATEGORY: ungrouped });
  assert.equal(bookmark.Category, ungrouped, 'inserting a row must start with an explicit bookmark type');
  Object.assign(bookmark, { Name: 'New bookmark', URL: 'https://new.invalid/', Favorite: true, Private: true });
  const { context } = editorHarness([bookmark], [0], []);
  context.addEditorValidationTarget = () => assert.fail('a new ungrouped bookmark must pass validation');
  const start = page.indexOf('function validateEditorDataBeforeSubmit');
  vm.runInNewContext(page.slice(start, page.indexOf('getEditorButtonDefaultLabel(backupDownloadButton)', start)), context);
  for (const category of [ungrouped, '', '   ', null]) {
    bookmark.Category = category;
    assert.equal(context.validateEditorDataBeforeSubmit(), null);
    const fields = context.exportBookmarksCSV().split(',');
    assert.equal(fields[4], ungrouped, 'blank types must never be submitted as legacy applications');
    assert.deepEqual(fields.slice(8), ['true', 'true']);
    assert.equal(bookmark.Favorite, true);
  }
});

test('link checks preserve moved rows and subsequent edits do not replace another bookmark', () => {
  const source = ['Alpha', 'Bravo', 'Charlie'].map((Name) => ({ Name, URL: `https://${Name.toLowerCase()}.invalid`, Category: 'Group' }));
  const order = [1, 2, 0];
  const { context, hooks } = editorHarness(source, order);
  context.applyLinkCheckResults([{ row: 1, url: source[1].URL, status: 'invalid', reason: '404' }]);
  assert.deepEqual(Array.from(context.getVisualBookmarkRows(), (row) => row.Name), ['Bravo', 'Charlie', 'Alpha']);
  assert.equal(source[1].CheckResult, '404');
  assert.equal(source[0].CheckResult, '');
  source[order[0]].Desc = 'edited';
  hooks.afterChange([[0, 'Desc', '', 'edited']], 'edit');
  assert.deepEqual(Array.from(context.getVisualBookmarkRows(), (row) => row.Name), ['Bravo', 'Charlie', 'Alpha']);
  const rows = context.exportBookmarksCSV().split('\r\n');
  assert.equal(rows.length, 3);
  assert(rows[0].includes('Bravo,https://bravo.invalid'));
  assert(rows[0].includes('edited'));
  assert(rows[1].includes('Charlie,https://charlie.invalid'));
  assert(rows[2].includes('Alpha,https://alpha.invalid'));
  source[order[0]].URL = 'https://new.invalid';
  hooks.afterChange([[0, 'URL', 'https://bravo.invalid', 'https://new.invalid']], 'edit');
  assert.equal(source[1].CheckResult, '', 'editing the URL invalidates the old check result for the same row');
});

test('link checks ignore stale URLs and invalid row numbers instead of marking another bookmark', () => {
  const source = [
    { Name: 'Restored A', URL: 'https://a.invalid/', Category: 'Group' },
    { Name: 'Checked B', URL: ' https://b.invalid/ ', Category: 'Group' },
  ];
  const { context } = editorHarness(source, [0, 1]);
  const results = [
    { row: 1, url: 'https://b.invalid/' },
    { row: 0, url: 'https://a.invalid/' },
    { row: -1, url: 'https://a.invalid/' },
    { row: 1.5, url: 'https://a.invalid/' },
    { url: 'https://a.invalid/' },
    { row: 3, url: 'https://a.invalid/' },
    { row: 1 },
  ].map((result) => ({ ...result, status: 'invalid', reason: '404' }));
  results.push({ row: 2, url: 'https://b.invalid/', status: 'restricted', reason: '403' });
  const summary = context.applyLinkCheckResults(results);
  assert.equal(source[0].CheckResult, '');
  assert.equal(source[0].CheckStatus, '');
  assert.equal(source[1].CheckResult, '403');
  assert.equal(source[1].CheckStatus, 'restricted');
  assert.equal(summary.invalidCount, 0);
  assert.equal(summary.restrictedCount, 1);
});

test('batch category swaps update each original bookmark group once and can be reversed', () => {
  const source = [
    { Name: 'Alpha', Category: 'A' },
    { Name: 'Bravo', Category: 'B' },
    { Name: 'Ungrouped', Category: '[SuperFlare \u672a\u5206\u7c7b]' },
  ];
  const { context, rename } = editorHarness(source, [1, 0, 2], ['A', 'B']);
  assert.equal(rename([[0, 'Name', 'A', ' B '], [1, 'Name', 'B', 'A']]), true);
  assert.deepEqual(source.map((row) => row.Category), ['B', 'A', context.FLARE_UNGROUPED_CATEGORY]);
  assert.equal(rename([[0, 'Name', 'B', 'A'], [1, 'Name', 'A', 'B']]), true);
  assert.deepEqual(source.map((row) => row.Category), ['A', 'B', context.FLARE_UNGROUPED_CATEGORY]);
  assert.deepEqual(Array.from(context.getVisualBookmarkRows(), (row) => row.Name), ['Bravo', 'Alpha', 'Ungrouped']);
});

test('category renames migrate edit and removed-row history in both undo stacks', () => {
  const edit = { actionType: 'change', changes: [[0, 3, 'A', 'B'], [0, 0, 'B', 'Title']] };
  const removed = { actionType: 'remove_row', data: [['Removed', 'https://removed.invalid', '', 'B']] };
  const redo = { actionType: 'change', changes: [[1, 3, 'B', 'A']] };
  const history = { doneActions: [edit, removed], undoneActions: [redo] };
  const source = [{ Name: 'Title', Category: 'B' }];
  const { rename } = editorHarness(source, [0], ['A', 'B'], history);
  assert.equal(rename([[1, 'Name', 'B', 'C']]), true);
  assert.equal(source[0].Category, 'C');
  source[0].Category = edit.changes[0][2];
  assert.equal(source[0].Category, 'A', 'undo preserves the original category');
  source[0].Category = edit.changes[0][3];
  assert.equal(source[0].Category, 'C', 'redo uses the renamed category');
  assert.equal(removed.data[0][3], 'C', 'deleted rows restore into the renamed category');
  assert.equal(redo.changes[0][2], 'C');
  assert.equal(edit.changes[1][2], 'B', 'unrelated values are unchanged');
  assert.equal(history.doneActions.length, 2);
  assert.equal(history.undoneActions.length, 1);
  assert.equal(rename([[1, 'Name', 'C', 'B']]), true);
  assert.equal(removed.data[0][3], 'B', 'undoing the rename updates history again');
  assert.equal(rename([[0, 'Name', 'A', 'B'], [1, 'Name', 'B', 'A']]), true);
  assert.deepEqual(edit.changes[0], [0, 3, 'B', 'A'], 'batch swaps apply once');
});

test('categories referenced only by removed rows cannot be cleared', () => {
  const history = { doneActions: [{ actionType: 'remove_row', data: [['Removed', '', '', 'A']] }], undoneActions: [] };
  const { rename } = editorHarness([], [], ['A'], history);
  assert.equal(rename([[0, 'Name', 'A', '']]), false);
  assert.equal(rename([[0, 'Name', 'A', 'Renamed']]), true);
  assert.equal(history.doneActions[0].data[0][3], 'Renamed');
});

test('invalid category batches leave category names and bookmark membership untouched', () => {
  const source = [{ Name: 'Alpha', Category: 'A' }, { Name: 'Bravo', Category: 'B' }];
  const { context, rename, notices } = editorHarness(source, [0, 1], ['A', 'B']);
  assert.equal(rename([[0, 'Name', 'A', 'B']]), false);
  assert.match(notices.at(-1).text, /“B” is already in use/);
  assert.equal(rename([[0, 'Name', 'A', 'C'], [1, 'Name', 'B', context.FLARE_UNGROUPED_CATEGORY]]), false);
  assert.match(notices.at(-1).text, / is reserved\./);
  assert.deepEqual(Array.from(context.getVisualCategoryRows(), (row) => row.Name), ['A', 'B']);
  assert.deepEqual(source.map((row) => row.Category), ['A', 'B']);
});

test('editing in English explains which bookmark cell must be filled', () => {
  const { context } = editorHarness([{ Name: 'Docs', URL: '', Category: 'Group' }], [0]);
  const error = context.validateEditorDataBeforeSubmit();
  assert.equal(error.target.prop, 'URL');
  assert.match(error.message, /^Bookmark row 1 needs a URL\./);
});

test('clearing an occupied category is rejected without losing favorites or undo membership', () => {
  const source = [{ Name: 'Alpha', URL: 'https://alpha.invalid', Category: 'A', Favorite: true, Private: true }];
  const { context, rename } = editorHarness(source, [0], ['A', 'Unused']);
  assert.equal(rename([[0, 'Name', 'A', 'Renamed']]), true);
  assert.equal(rename([[0, 'Name', 'Renamed', '   ']]), false);
  assert.equal(source[0].Category, 'Renamed');
  assert.equal(rename([[0, 'Name', 'Renamed', 'A']]), true);
  assert.equal(source[0].Category, 'A');
  assert.equal(source[0].Favorite, true);
  assert.equal(source[0].Private, true);
  assert.match(context.exportBookmarksCSV(), /,A,,,,true,true$/);
  assert.equal(rename([[1, 'Name', 'Unused', '']]), true);
});

test('a batch containing a cleared occupied category is rejected atomically', () => {
  const source = [{ Name: 'Alpha', URL: 'https://alpha.invalid', Category: 'A' }];
  const { context, rename } = editorHarness(source, [0], ['A', 'B']);
  assert.equal(rename([[1, 'Name', 'B', 'C'], [0, 'Name', 'A', '']]), false);
  assert.deepEqual(Array.from(context.getVisualCategoryRows(), (row) => row.Name), ['A', 'B']);
  assert.equal(source[0].Category, 'A');
});

test('new categories remain editable and cleared bookmark rows retain their group for undo', () => {
  const source = [{ Name: '', URL: '', Category: 'Group', Favorite: true }];
  const { context, rename } = editorHarness(source, [0], ['Group', '']);
  assert.equal(rename([[0, 'Name', 'Group', '']]), false);
  assert.equal(rename([[1, 'Name', '', 'New']]), true);
  assert.equal(rename([[1, 'Name', 'New', '']]), true);
  source[0].Name = 'Restored';
  source[0].URL = 'https://example.com/';
  assert.equal(source[0].Category, 'Group');
  assert.equal(source[0].Favorite, true);
  assert.deepEqual(Array.from(context.getVisualCategoryRows(), (row) => row.Name), ['Group', '']);
});

test('application conversion records favorites in the same reversible change batch', () => {
  const source = [{ Name: 'Saved', Category: 'Group', Favorite: true }, { Name: 'App', Category: '[SuperFlare \u5e94\u7528]', Favorite: false }];
  const { context, hooks } = editorHarness(source, [0, 1]);
  const original = structuredClone(source);
  const changes = [[0, 'Category', 'Group', context.FLARE_FIX_CATEGORY[0]]];
  const apply = (batch) => {
    hooks.beforeChange(batch);
    for (const [row, prop, , value] of batch) source[row][prop] = value;
  };
  apply(changes);
  assert.equal(changes.length, 2, 'the category edit must include its favorite change');
  assert.equal(source[0].Favorite, false);
  const inverse = changes.map(([row, prop, before, after]) => [row, prop, after, before]);
  apply(inverse);
  assert.deepEqual(source, original, 'one undo must restore the complete original row');
  apply(changes);
  assert.equal(source[0].Category, context.FLARE_FIX_CATEGORY[0]);
  assert.equal(source[0].Favorite, false, 'redo must preserve the application invariant');

  apply([[1, 'Favorite', false, true], [1, 'Category', context.FLARE_FIX_CATEGORY[0], 'Group']]);
  assert.equal(source[1].Favorite, true, 'a batch converting an app to a bookmark may favorite it');
  apply([[0, 'Favorite', false, 'true']]);
  assert.equal(source[0].Favorite, false, 'pasting a favorite into an application must still be rejected');
});

test('pasted checkboxes and saved CSV agree, and invalid text cannot clear privacy', () => {
  const source = [{ Name: 'Private link', URL: 'https://example.invalid/', Category: 'Group', Private: false, Favorite: true }];
  const { context, hooks } = editorHarness(source, [0]);
  const apply = (changes) => {
    hooks.beforeChange(changes);
    for (const [, prop, , value] of changes) source[0][prop] = value;
  };
  apply([[0, 'Private', false, ' TRUE '], [0, 'Favorite', true, 'false']]);
  assert.equal(source[0].Private, true);
  assert.equal(source[0].Favorite, false);
  apply([[0, 'Private', true, 'unrecognized']]);
  assert.match(context.exportBookmarksCSV(), /,Group,,,,true,false$/);
});

test('LAN dropdown selection normalizes inside one reversible change batch', () => {
  const source = [
    { Name: 'Other', Category: 'Group', LocalURL: 'http://192.0.2.2:9000' },
    { Name: 'Selected', Category: 'Group', LocalURL: '' },
  ];
  const order = [1, 0];
  const { context, hooks } = editorHarness(source, order);
  context.localPortValueMap['\u30108080\u3011Service'] = 'http://192.0.2.1:8080';
  const changes = [[0, 'LocalURL', '', '\u30108080\u3011Service'], [1, 'Desc', '', 'Edited']];
  const apply = (batch) => {
    hooks.beforeChange(batch);
    for (const [row, prop, , value] of batch) source[order[row]][prop] = value;
    hooks.afterChange(batch, 'edit');
  };
  apply(changes);
  assert.equal(changes.length, 2, 'normalization must not create a second write');
  assert.equal(changes[0][3], 'http://192.0.2.1:8080', 'the undo action must contain the URL, not the display label');
  assert.equal(source[1].LocalURL, 'http://192.0.2.1:8080');
  assert.equal(source[0].LocalURL, 'http://192.0.2.2:9000');
  apply(changes.map(([row, prop, before, after]) => [row, prop, after, before]));
  assert.equal(source[1].LocalURL, '', 'one undo restores the previous value');
  apply(changes);
  assert.equal(source[1].LocalURL, 'http://192.0.2.1:8080', 'one redo restores the actual URL');
  apply([[0, 'LocalURL', source[1].LocalURL, 'http://custom.invalid:8888']]);
  assert.equal(source[1].LocalURL, 'http://custom.invalid:8888', 'manual URLs must remain editable');
});

test('link-check locks block undo and redo in both tables without clearing either history', () => {
  const tables = ['categories', 'bookmarks'].map((name) => {
    const settings = { readOnly: false };
    const history = {
      doneActions: [{ actionType: 'remove_row' }],
      undoneActions: [{ actionType: 'change' }],
      ignoreNewActions: false,
      undo() { this.doneActions.pop(); this.undoCalls = (this.undoCalls || 0) + 1; },
      redo() { this.undoneActions.pop(); this.redoCalls = (this.redoCalls || 0) + 1; },
    };
    return { name, history, getSettings: () => settings, getPlugin: () => history, updateSettings: (next) => Object.assign(settings, next) };
  });
  const context = {
    addCategoryButton: {}, addBookmarkButton: {},
    instanceCategories: tables[0], instanceBookmarks: tables[1],
    BOOKMARK_CONTEXT_MENU_ITEMS: ['undo', 'redo'],
  };
  for (const [startText, endText] of [
    ['function setBookmarkTableLinkCheckLocked', 'function appendEditorRow'],
    ['function guardEditorHistoryReplay', 'preserveEditorRowRemovalHistory(instanceCategories'],
  ]) {
    const start = page.indexOf(startText);
    vm.runInNewContext(page.slice(start, page.indexOf(endText, start)), context);
  }
  tables.forEach((table) => context.guardEditorHistoryReplay(table));
  context.setBookmarkTableLinkCheckLocked(true);
  assert.equal(context.addCategoryButton.disabled, true);
  assert.equal(context.addBookmarkButton.disabled, true);
  for (const table of tables) {
    table.history.undo();
    table.history.redo();
    assert.equal(table.history.doneActions.length, 1, table.name + ' undo must retain its action');
    assert.equal(table.history.undoneActions.length, 1, table.name + ' redo must retain its action');
    assert.equal(table.history.ignoreNewActions, false);
  }
  context.setBookmarkTableLinkCheckLocked(false);
  assert.equal(context.addCategoryButton.disabled, false);
  assert.equal(context.addBookmarkButton.disabled, false);
  for (const table of tables) {
    table.history.undo();
    table.history.redo();
    assert.equal(table.history.undoCalls, 1);
    assert.equal(table.history.redoCalls, 1);
  }
});

test('blocked category undo and redo leave history intact and the next edit undoable', () => {
  const { context, rename } = editorHarness([{ Category: 'New' }], [0], ['New', 'Other']);
  const action = { actionType: 'change', changes: [[0, 0, '', 'New']] };
  let value = 'New';
  let replayed = 0;
  const history = {
    doneActions: [action], undoneActions: [], ignoreNewActions: false,
    undo() { replayed++; this.ignoreNewActions = true; this.doneActions.pop(); },
    redo() { replayed++; this.ignoreNewActions = true; this.undoneActions.pop(); },
  };
  const table = {
    getPlugin: () => history,
    colToProp: () => 'Name',
    getDataAtRowProp: () => value,
    getSettings: () => ({ beforeChange: context.categoryHooks.beforeChange }),
  };
  const start = page.indexOf('function guardEditorHistoryReplay');
  const end = page.indexOf('preserveEditorRowRemovalHistory(instanceCategories', start);
  vm.runInNewContext(page.slice(start, end), context);
  context.guardEditorHistoryReplay(table);
  history.undo();
  history.undo();
  assert.equal(replayed, 0);
  assert.equal(history.doneActions[0], action, 'blocked undo must retain the original action');
  assert.equal(history.ignoreNewActions, false);
  assert.equal(rename([[1, 'Name', 'Other', 'Edited']]), true);
  const edit = { actionType: 'change', changes: [[1, 0, 'Other', 'Edited']] };
  if (!history.ignoreNewActions) history.doneActions.push(edit);
  history.undo();
  assert.equal(replayed, 1, 'the next valid edit must replay normally');
  assert.equal(history.doneActions[0], action);

  const redo = { actionType: 'change', changes: [[1, 0, 'Other', 'New']] };
  history.undoneActions = [redo];
  history.ignoreNewActions = false;
  value = 'Edited';
  history.redo();
  assert.equal(replayed, 1, 'redo into a duplicate category must also be blocked before replay');
  assert.equal(history.undoneActions[0], redo);
  assert.equal(history.ignoreNewActions, false);
});

test('removed moved rows restore physical identities, renamed groups and hidden state', () => {
  const original = [
    { Name: 'A', Category: 'Group', Favorite: true, CheckStatus: 'valid' },
    { Name: 'B', Category: 'Group', Favorite: false, CheckStatus: 'invalid' },
    { Name: 'C', Category: 'Group', Favorite: false },
  ];
  let source = structuredClone(original);
  let order = [2, 0, 1];
  const removed = [2, 0];
  const props = ['Name', 'Category', 'Favorite'];
  const action = { index: 0, actionType: 'remove_row', data: [['C']], fixedRowsTop: 0, fixedRowsBottom: 0 };
  const history = { doneActions: [action], ignoreNewActions: false };
  const hooks = {};
  let restored = 0;
  let completed = 0;
  const settings = {};
  const table = {
    addHook: (name, callback) => { hooks[name] = callback; },
    getPlugin: () => history,
    getSourceDataAtRow: (row) => source[row],
    getSourceData: () => source,
    getSettings: () => settings,
    countCols: () => props.length,
    colToProp: (col) => props[col],
    rowIndexMapper: {
      getIndexesSequence: () => order,
      setIndexesSequence: (next) => { order = Array.from(next); },
    },
    alter: (operation, index, amount, changeSource) => {
      assert.equal(operation, 'insert_row');
      assert.equal(changeSource, 'UndoRedo.undo');
      source.splice(index, 0, ...Array.from({ length: amount }, () => ({})));
    },
    setSourceDataAtCell: (changes) => {
      for (const [row, prop, value] of changes) source[row][prop] = value;
    },
  };
  const context = {};
  const start = page.indexOf('function preserveEditorRowRemovalHistory');
  vm.runInNewContext(page.slice(start, page.indexOf('function guardEditorHistoryReplay', start)), context);
  context.preserveEditorRowRemovalHistory(table, () => { restored++; });
  hooks.beforeRemoveRow(0, 2, removed, 'edit');
  assert.deepEqual(Array.from(action.data, (row) => row[0]), ['C', 'A']);
  source = [source[1]];
  order = [0];
  action.data[0][1] = 'Renamed';
  action.undo(table, () => { completed++; });
  assert.deepEqual(source.map((row) => row.Name), ['A', 'B', 'C']);
  assert.deepEqual(order, [2, 0, 1], 'earlier moves depend on the original physical identities');
  assert.deepEqual(source.map((row) => row.CheckStatus), original.map((row) => row.CheckStatus));
  assert.equal(source[0].Favorite, true);
  assert.equal(source[2].Category, 'Renamed');
  assert.equal(restored, 1);
  assert.equal(completed, 1);
  const undo = action.undo;
  hooks.beforeRemoveRow(0, 2, removed, 'UndoRedo.redo');
  assert.equal(action.undo, undo, 'redo must retain the original snapshot');
});
