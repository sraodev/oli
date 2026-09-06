// Run with Playwright available on NODE_PATH. Uses only synthetic HTTP fixtures.
// BROWSER_CHANNEL=chrome uses installed Chrome; ARTIFACT_DIR saves screenshots.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const GiB = 1024 ** 3;
const token = 'synthetic-dashboard-test-token';
const assets = path.join(__dirname, '../internal/dashboard/static');
let slowScan = false;
let cleanCalls = 0;
let cleanMode = 'changed';
const categories = [
  { rule_id: 'user-caches', name: 'User caches', description: 'Rebuildable application caches. Review candidates before removing them.', risk: 'low', age: 'Older than 7 days', action: 'clean', item_count: 2, detected_bytes: 3 * GiB, reclaimable_bytes: 2 * GiB, selected_by_default: true, largest_items: [{ name: 'preview-cache', location: '~/Library/Caches/Example/preview-cache', size_bytes: 2 * GiB, modified_age: '14 days ago' }] },
  { rule_id: 'trash', name: 'Trash', description: 'Emptying Trash permanently removes the selected candidates.', risk: 'high', age: 'Explicit review', action: 'clean', item_count: 1, detected_bytes: GiB, reclaimable_bytes: GiB, selected_by_default: false },
  { rule_id: 'downloads', name: 'Downloads', description: 'Personal files are discovery only. Nothing is selected for cleanup.', risk: 'high', age: 'Any age', action: 'scan_only', item_count: 2, detected_bytes: 12 * GiB, reclaimable_bytes: 0, selected_by_default: false },
];
const server = http.createServer(async (req, res) => {
  const files = { '/': ['index.html', 'text/html'], '/assets/app.css': ['app.css', 'text/css'], '/assets/app.js': ['app.js', 'text/javascript'] };
  if (files[req.url]) {
    const [file, type] = files[req.url];
    res.writeHead(200, { 'Content-Type': type });
    res.end(fs.readFileSync(path.join(assets, file)));
    return;
  }
  res.setHeader('Content-Type', 'application/json');
  if (req.headers.authorization !== `Bearer ${token}`) { res.writeHead(401); res.end('{}'); return; }
  if (req.url === '/api/disk') {
    res.end(JSON.stringify({ volume: 'Fixture volume', total_bytes: 500 * GiB, free_bytes: 80 * GiB, used_bytes: 420 * GiB }));
  } else if (req.url === '/api/scan') {
    res.setHeader('Content-Type', 'application/x-ndjson');
    res.write(JSON.stringify({ type: 'scan.started', scan_id: 'fixture-scan' }) + '\n');
    if (slowScan) { res.on('close', () => res.end()); return; }
    for (const category of categories) res.write(JSON.stringify({ type: 'scan.category', category }) + '\n');
    res.write(JSON.stringify({ type: 'scan.issue', issue: { message: 'A fixture folder is inaccessible; these are partial findings.' } }) + '\n');
    res.end(JSON.stringify({ type: 'scan.complete', summary: { scan_id: 'fixture-scan', files_scanned: 5, reclaimable_bytes: 3 * GiB, duration_millis: 1250 } }) + '\n');
  } else if (req.url === '/api/explore') {
    const metrics = { allocated_bytes: 12 * GiB, logical_bytes: 12 * GiB, unique_files: 2, symlinks: 1 };
    const item = { display_path: '~/Downloads/' + 'long-folder-name-'.repeat(12) + '/archive.zip', modified: '2025-01-01T00:00:00Z', metrics };
    res.end(JSON.stringify({ partial: true, entries_inspected: 3, total: metrics, scopes: [{ name: 'Downloads', display_path: '~/Downloads', metrics }], folders: [item], large_files: [item], old_files: [], min_size_bytes: GiB, older_than_days: 180, limit: 50, warnings: [{ display_path: '~/Downloads/Restricted', message: 'Permission denied. No files changed.' }] }));
  } else if (req.url === '/api/clean') {
    cleanCalls++;
    let body = '';
    for await (const chunk of req) body += chunk;
    const request = JSON.parse(body);
    assert.equal(request.confirmation, 'DELETE');
    assert.deepEqual(request.rule_ids, ['user-caches']);
    if (cleanMode === 'complete') {
      res.setHeader('Content-Type', 'application/x-ndjson');
      res.end(JSON.stringify({ type: 'clean.complete', summary: { selected_bytes: 2 * GiB, removed_bytes: 2 * GiB, measured_reclaimed_bytes: 2 * GiB, removed_items: 2, failed_items: 0, duration_millis: 500 } }) + '\n');
      return;
    }
    res.writeHead(409);
    res.end(JSON.stringify({ error: 'Fixture changed since scan. Scan again before cleanup.' }));
  } else { res.writeHead(404); res.end('{}'); }
});

async function noOverflow(page, label, fixedDock = true) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const overflow = await page.evaluate(() => ({ width: document.documentElement.scrollWidth, viewport: innerWidth, elements: [...document.querySelectorAll('main *, dialog *')].filter(el => el.getBoundingClientRect().right > innerWidth + 1).map(el => el.id || el.className || el.tagName) }));
  assert(overflow.width <= overflow.viewport, `${label}: page overflow ${JSON.stringify(overflow)}`);
  const dock = await page.locator('#cleanup-dock').boundingBox();
  if (dock) {
    const viewport = page.viewportSize();
    assert(dock.x >= 0 && dock.x + dock.width <= viewport.width + 1, `${label}: dock overflows`);
    if (fixedDock) assert(dock.y >= 0 && dock.y + dock.height <= viewport.height, `${label}: dock clipped`);
    for (const selector of ['#clean-button', '#confirmation-input']) {
      const box = await page.locator(selector).boundingBox();
      assert(box.x >= dock.x && box.x + box.width <= dock.x + dock.width, `${label}: control outside dock`);
    }
  }
}

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const url = `http://127.0.0.1:${server.address().port}`;
  let browser;
  try {
    browser = await chromium.launch({ headless: true, channel: process.env.BROWSER_CHANNEL || undefined });
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(url);
    assert(await page.locator('#auth-notice').isVisible());
    assert(await page.locator('#scan-button').isDisabled());
    await page.goto(`${url}/#token=${token}`);
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#disk-free').textContent === '80 GiB');
    assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme), 'light');
    await page.locator('input[value="safe"]').focus();
    await page.keyboard.press('ArrowRight');
    assert(await page.locator('input[value="balanced"]').isChecked());
    await page.keyboard.press('ArrowLeft');
    await page.locator('#scan-button').click();
    await page.waitForFunction(() => document.querySelector('#scan-progress-title').textContent === 'Scan complete');
    assert.equal(await page.locator('.category-card').count(), 3);
    assert(await page.locator('#scan-issues').isVisible());
    assert(await page.locator('.rule-checkbox[value="downloads"]').isDisabled());
    assert(!(await page.locator('.rule-checkbox[value="trash"]').isChecked()));
    assert(await page.locator('#clean-button').isDisabled());
    await page.locator('#confirmation-input').fill('delete');
    assert(await page.locator('#clean-button').isDisabled());
    await page.locator('#confirmation-input').fill('DELETE');
    assert(await page.locator('#clean-button').isEnabled());
    await page.locator('#select-none').click();
    assert.equal(await page.locator('#confirmation-input').inputValue(), '');
    assert(await page.locator('#clean-button').isDisabled());
    await page.locator('#select-safe').click();
    await page.locator('[data-rule-id="user-caches"] .largest-items summary').focus();
    await page.keyboard.press('Enter');
    assert(await page.locator('[data-rule-id="user-caches"] .largest-items').getAttribute('open') !== null);

    for (const [width, height] of [[1440, 1000], [1024, 900], [768, 1024], [390, 844], [320, 740]]) {
      await page.setViewportSize({ width, height });
      await page.locator('a[href="#storage-atlas"]').click();
      await page.waitForFunction(() => document.querySelector('a[href="#storage-atlas"]').getAttribute('aria-current') === 'location');
      await page.locator('#atlas-button').click();
      await page.waitForFunction(() => document.querySelector('#atlas-status').textContent.startsWith('Partial report'));
      await noOverflow(page, `${width}px`);
      await page.locator('[data-atlas-list="old_files"]').click();
      assert.match(await page.locator('#atlas-items').innerText(), /No matching items/);
      await page.locator('[data-atlas-list="folders"]').click();
      await page.locator('a[href="#overview"]').click();
      await page.evaluate(() => scrollTo(0, 0));
      if (process.env.ARTIFACT_DIR) {
        fs.mkdirSync(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `dashboard-${width}.png`), fullPage: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `viewport-${width}.png`) });
      }
      console.log(`PASS ${width}px: navigation, findings, confirmation dock, no page overflow`);
    }
    await page.setViewportSize({ width: 844, height: 390 });
    await noOverflow(page, 'landscape', false);
    assert.equal(await page.locator('#cleanup-dock').evaluate(el => getComputedStyle(el).position), 'static');
    console.log('PASS short landscape: confirmation remains in document flow');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.locator('#confirmation-input').fill('DELETE');
    await page.locator('#clean-button').click();
    await page.waitForFunction(() => document.querySelector('#activity-log').textContent.includes('Fixture changed'));
    assert.equal(cleanCalls, 1);
    assert(!(await page.locator('#outcome-dialog').isVisible()));
    slowScan = true;
    await page.locator('#scan-button').click();
    await page.waitForFunction(() => document.querySelector('#scan-button').textContent.includes('Stop scan'));
    await page.locator('#scan-button').click();
    await page.waitForFunction(() => document.querySelector('#scan-issues').textContent.includes('Scan cancelled'));
    assert(await page.locator('#clean-button').isDisabled());
    slowScan = false;
    cleanMode = 'complete';
    await page.locator('#scan-button').click();
    await page.waitForFunction(() => document.querySelector('#scan-progress-title').textContent === 'Scan complete');
    await page.locator('#confirmation-input').fill('DELETE');
    await page.locator('#confirmation-input').press('Enter');
    await page.locator('#outcome-dialog').waitFor({ state: 'visible' });
    assert.equal(await page.locator('#measured-total').innerText(), '2 GiB');
    await page.setViewportSize({ width: 320, height: 740 });
    await noOverflow(page, 'outcome');
    await page.getByRole('button', { name: 'Done', exact: true }).click();
    assert(!(await page.locator('#outcome-dialog').isVisible()));
    assert.deepEqual(errors, []);
    console.log('PASS locked session, keyboard profiles/disclosure, explicit confirmation, selection reset, changed-file failure, cancellation, outcome dialog, no script errors');
  } finally {
    if (browser) await browser.close();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
