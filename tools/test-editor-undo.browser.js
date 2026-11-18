// Run in an isolated /editor page with the bundled Handsontable loaded.
// These regressions replace the in-memory draft and clear its undo history; they never save it.
async function runEditorUndoRegressionTests() {
    const savedCategories = instanceCategories.getSourceData().map(row => Object.assign({}, row));
    const savedBookmarks = getVisualBookmarkRows();
    const results = [];
    const clone = value => JSON.parse(JSON.stringify(value));
    const equal = (actual, expected, message) => {
        if (JSON.stringify(actual) !== JSON.stringify(expected)) {
            throw new Error(message + ': ' + JSON.stringify({ actual, expected }));
        }
    };
    const settle = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const fixture = ['A', 'B', 'C'].map((name, index) => ({
        Name: name, URL: 'https://example.invalid/' + name, LocalURL: '', Category: ['Alpha', 'Bravo', 'Charlie'][index],
        Subdir: '', Icon: '', Desc: name + ' description', Private: index === 2, Favorite: index === 0,
        CheckResult: 'Result ' + name, CheckStatus: 'valid'
    }));
    async function reset() {
        instanceCategories.loadData(['Alpha', 'Bravo', 'Charlie'].map((Name, index) => ({ ID: String(index + 1), Name })));
        syncCategoriesStateFromTable();
        instanceBookmarks.loadData(clone(fixture));
        syncBookmarksStateFromTable();
        updateDataTable();
        await settle();
    }
    async function test(name, run) {
        await reset();
        await run();
        results.push(name);
    }
    try {
        await test('new bookmarks default to ungrouped and keep favorite state in the save payload', async () => {
            appendEditorRow(instanceBookmarks);
            const row = instanceBookmarks.countRows() - 1;
            equal(instanceBookmarks.getDataAtRowProp(row, 'Category'), FLARE_UNGROUPED_CATEGORY, 'new bookmark type');
            instanceBookmarks.setDataAtRowProp(row, 'Name', 'New bookmark');
            instanceBookmarks.setDataAtRowProp(row, 'URL', 'https://new.invalid/');
            instanceBookmarks.setDataAtRowProp(row, 'Favorite', true);
            await settle();
            equal(validateEditorDataBeforeSubmit(), null, 'valid new bookmark');
            const fields = exportBookmarksCSV().split('\r\n').at(-1).split(',');
            equal(fields[4], FLARE_UNGROUPED_CATEGORY, 'saved bookmark type');
            equal(fields[9], 'true', 'saved favorite');
        });
        await test('link checks block undo and redo in both tables and preserve their history', async () => {
            const tables = [instanceCategories, instanceBookmarks];
            for (const table of tables) {
                table.setDataAtRowProp(0, 'Name', 'Edited first');
                table.setDataAtRowProp(1, 'Name', 'Edited second');
                table.undo();
            }
            await settle();
            const before = tables.map(table => ({
                rows: clone(table.getSourceData()),
                done: table.getPlugin('undoRedo').doneActions.slice(),
                undone: table.getPlugin('undoRedo').undoneActions.slice()
            }));
            setBookmarkTableLinkCheckLocked(true);
            try {
                for (const [index, table] of tables.entries()) {
                    table.undo();
                    table.redo();
                    await settle();
                    const history = table.getPlugin('undoRedo');
                    equal(table.getSourceData(), before[index].rows, 'locked rows');
                    equal(history.doneActions.length, before[index].done.length, 'locked undo stack');
                    equal(history.undoneActions.length, before[index].undone.length, 'locked redo stack');
                    equal(history.ignoreNewActions, false, 'history remains usable');
                }
            } finally {
                setBookmarkTableLinkCheckLocked(false);
            }
            for (const table of tables) {
                table.redo();
                await settle();
                equal(table.getDataAtRowProp(1, 'Name'), 'Edited second', 'redo after check');
                table.undo();
                table.undo();
                await settle();
            }
            equal(getVisualCategoryRows().map(row => row.Name), ['Alpha', 'Bravo', 'Charlie'], 'category undo after check');
            equal(getVisualBookmarkRows(), fixture, 'bookmark undo after check');
        });
        await test('application conversion undoes and redoes category and favorite atomically', async () => {
            instanceBookmarks.setDataAtRowProp(0, 'Category', FLARE_FIX_CATEGORY[0]);
            await settle();
            equal(instanceBookmarks.getDataAtRowProp(0, 'Favorite'), false, 'application favorite');
            equal(instanceBookmarks.getPlugin('undoRedo').doneActions.length, 1, 'one action');
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), fixture, 'complete undo');
            instanceBookmarks.redo();
            await settle();
            equal(instanceBookmarks.getDataAtRowProp(0, 'Category'), FLARE_FIX_CATEGORY[0], 'redo category');
            equal(instanceBookmarks.getDataAtRowProp(0, 'Favorite'), false, 'redo favorite');
        });
        await test('LAN port selection undoes and redoes the URL in one action', async () => {
            const label = '\u30108080\u3011Undo regression';
            const previous = localPortValueMap[label];
            localPortValueMap[label] = 'http://192.0.2.1:8080';
            try {
                instanceBookmarks.setDataAtRowProp(0, 'LocalURL', label);
                await settle();
                equal(instanceBookmarks.getDataAtRowProp(0, 'LocalURL'), 'http://192.0.2.1:8080', 'resolved URL');
                equal(instanceBookmarks.getPlugin('undoRedo').doneActions.length, 1, 'one port selection action');
                instanceBookmarks.undo();
                await settle();
                equal(instanceBookmarks.getDataAtRowProp(0, 'LocalURL'), '', 'one undo');
                instanceBookmarks.redo();
                await settle();
                equal(instanceBookmarks.getDataAtRowProp(0, 'LocalURL'), 'http://192.0.2.1:8080', 'one redo');
            } finally {
                if (previous === undefined) {
                    delete localPortValueMap[label];
                } else {
                    localPortValueMap[label] = previous;
                }
            }
        });
        await test('moved-row deletion preserves all fields and earlier move history', async () => {
            instanceBookmarks.getPlugin('manualRowMove').moveRows([2], 0);
            instanceBookmarks.render();
            instanceBookmarks.alter('remove_row', 0, 2);
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), [fixture[2], fixture[0], fixture[1]], 'deleted records restored');
            equal(instanceBookmarks.rowIndexMapper.getIndexesSequence(), [2, 0, 1], 'physical row identities');
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), fixture, 'undo earlier move');
            instanceBookmarks.redo();
            instanceBookmarks.redo();
            await settle();
            equal(getVisualBookmarkRows(), [fixture[1]], 'redo move and deletion');
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), [fixture[2], fixture[0], fixture[1]], 'repeat restore');
        });
        await test('deleting all moved rows can be undone without losing records', async () => {
            instanceBookmarks.getPlugin('manualRowMove').moveRows([2], 0);
            instanceBookmarks.render();
            instanceBookmarks.alter('remove_row', 0, 3);
            equal(instanceBookmarks.countRows(), 0, 'all rows deleted');
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), [fixture[2], fixture[0], fixture[1]], 'restore empty table');
        });
        await test('category table restores moved deleted rows and bookmark membership', async () => {
            instanceCategories.getPlugin('manualRowMove').moveRows([2], 0);
            instanceCategories.render();
            instanceCategories.alter('remove_row', 0, 2);
            instanceCategories.undo();
            await settle();
            equal(getVisualCategoryRows().map(row => row.Name), ['Charlie', 'Alpha', 'Bravo'], 'category restore');
            instanceCategories.undo();
            await settle();
            equal(getVisualCategoryRows().map(row => row.Name), ['Alpha', 'Bravo', 'Charlie'], 'undo category move');
            equal(getVisualBookmarkRows(), fixture, 'bookmark membership unchanged');
        });
        await test('restored deleted bookmarks use renamed categories', async () => {
            instanceBookmarks.getPlugin('manualRowMove').moveRows([2], 0);
            instanceBookmarks.render();
            instanceBookmarks.alter('remove_row', 0, 2);
            instanceCategories.setDataAtRowProp(2, 'Name', 'Renamed');
            instanceBookmarks.undo();
            await settle();
            equal(getVisualBookmarkRows(), [Object.assign({}, fixture[2], { Category: 'Renamed' }), fixture[0], fixture[1]], 'renamed removed row');
        });
        await test('rejected category undo retains history and the next edit remains undoable', async () => {
            instanceCategories.alter('insert_row', 3, 1);
            instanceCategories.setDataAtRowProp(3, 'Name', 'New');
            instanceBookmarks.setDataAtRowProp(0, 'Category', 'New');
            const history = instanceCategories.getPlugin('undoRedo');
            const originalHistory = history.doneActions.slice();
            instanceCategories.undo();
            instanceCategories.undo();
            await settle();
            equal(history.doneActions.length, originalHistory.length, 'blocked actions retained');
            equal(history.ignoreNewActions, false, 'history not disabled');
            instanceCategories.setDataAtRowProp(1, 'Name', 'Edited');
            equal(history.doneActions.length, originalHistory.length + 1, 'next edit recorded');
            instanceCategories.undo();
            await settle();
            equal(instanceCategories.getDataAtRowProp(1, 'Name'), 'Bravo', 'next edit undone');
            equal(instanceBookmarks.getDataAtRowProp(1, 'Category'), 'Bravo', 'linked category undone');
        });
        await test('rejected category redo retains history and later edits still work', async () => {
            instanceCategories.alter('insert_row', 3, 1);
            instanceCategories.setDataAtRowProp(3, 'Name', 'Unused');
            instanceCategories.setDataAtRowProp(3, 'Name', '');
            instanceCategories.undo();
            instanceBookmarks.setDataAtRowProp(0, 'Category', 'Unused');
            const history = instanceCategories.getPlugin('undoRedo');
            instanceCategories.redo();
            await settle();
            equal(instanceCategories.getDataAtRowProp(3, 'Name'), 'Unused', 'referenced category retained');
            equal(history.undoneActions.length, 1, 'blocked redo retained');
            equal(history.ignoreNewActions, false, 'redo did not disable history');
            instanceCategories.setDataAtRowProp(1, 'Name', 'Edited');
            instanceCategories.undo();
            await settle();
            equal(instanceCategories.getDataAtRowProp(1, 'Name'), 'Bravo', 'edit after blocked redo');
        });
        return { passed: results.length, results };
    } finally {
        instanceCategories.loadData(savedCategories);
        syncCategoriesStateFromTable();
        instanceBookmarks.loadData(savedBookmarks);
        syncBookmarksStateFromTable();
        updateDataTable();
        await settle();
    }
}
