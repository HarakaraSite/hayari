#!/usr/bin/env node
// browser-e2e-smoke.mjs — headless-Chromium UI smoke with deterministic API mocks.
//
// Pre-tag reference check for .forgejo/release-profile.yml
// (pre_release.reference_checks → browser-e2e). It builds and starts a local
// hayari instance with a throwaway database, then drives the Web UI in headless
// Chromium. All /api/* responses are mocked in the browser, so the check needs
// no external feeds, no real article data, and leaves no state behind. The
// login page and static assets (HTML/CSS/JS) are served by the real binary so
// missing or broken assets are detected.
//
// Usage: node scripts/browser-e2e-smoke.mjs   (via scripts/browser-e2e-smoke.sh)

import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createRequire } from 'node:module';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

const USER = 'e2e-user';
const PASS = 'e2e-password';
const ASSETS = [
  '/stylesheets/base.css',
  '/stylesheets/app.css',
  '/javascripts/api.js',
  '/javascripts/key.js',
  '/javascripts/app.js',
];

function resolvePlaywright() {
  const candidates = process.env.PLAYWRIGHT_MODULE
    ? [process.env.PLAYWRIGHT_MODULE, 'playwright']
    : ['playwright'];
  for (const candidate of candidates) {
    try {
      return require(candidate);
    } catch {
      // try the next candidate
    }
  }
  console.error('browser-e2e: playwright not found.');
  console.error('Set PLAYWRIGHT_MODULE to the installed playwright module path.');
  process.exit(1);
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function waitFor(fn, message, timeoutMs = 5000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`timeout waiting for: ${message}`);
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

// ── Deterministic fixtures ─────────────────────────────────────────────────
const now = Date.now();

function makeItem(id, feedId) {
  return {
    id,
    feed_id: feedId,
    guid: `guid-${id}`,
    title: `Sample article ${id} with a headline long enough to wrap`,
    title_translation_state: 'none',
    link: `https://example.invalid/article/${id}`,
    date: new Date(now - id * 3600e3).toISOString(),
    content: `<p id="body-${id}">Body of article ${id}. Here is a <a href="https://example.invalid/">link</a> and a <a href="#body-${id}">jump</a>.</p>`,
    author: 'E2E Author',
    status: id % 2 === 1 ? 'unread' : 'read',
    starred: id === 107,
    image: null,
  };
}

const feed1Items = Array.from({ length: 45 }, (_, i) => makeItem(100 + i, 1));
const feed2Items = Array.from({ length: 3 }, (_, i) => makeItem(200 + i, 2));
const allItems = [...feed1Items, ...feed2Items];

const FOLDERS = [
  { id: 1, title: 'Tech News', is_expanded: true },
  { id: 2, title: 'Empty Folder', is_expanded: true },
];
const ICON = 'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIxNiIgaGVpZ2h0PSIxNiI+PHJlY3Qgd2lkdGg9IjE2IiBoZWlnaHQ9IjE2IiByeD0iMyIgZmlsbD0iIzAxNzJhZCIvPjwvc3ZnPg==';
const ICON_SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16" rx="3" fill="#0172ad"/></svg>';
const FEEDS = [
  {
    id: 1, folder_id: 1, title: 'Alpha Feed',
    feed_url: 'https://example.invalid/alpha.xml', site_url: 'https://example.invalid/',
    icon: ICON, last_refreshed: null, title_filter_keywords: '',
  },
  {
    id: 2, folder_id: null, title: 'Beta Feed',
    feed_url: 'https://example.invalid/beta.xml', site_url: 'https://example.invalid/',
    icon: ICON, last_refreshed: null, title_filter_keywords: '',
  },
];

async function main() {
  const { chromium } = resolvePlaywright();
  const tmp = mkdtempSync(join(tmpdir(), 'hayari-browser-e2e-'));
  let server;
  let browser;

  try {
    // Build and start the binary under test.
    const build = spawnSync('go', ['build', '-o', join(tmp, 'hayari'), './cmd/hayari'], {
      cwd: repoRoot,
      stdio: 'inherit',
    });
    assert(build.status === 0, 'go build failed');

    const port = await freePort();
    const base = `http://127.0.0.1:${port}`;
    server = spawn(
      join(tmp, 'hayari'),
      ['--addr', `127.0.0.1:${port}`, '--db', join(tmp, 'hayari.db'), '--user', USER, '--pass', PASS],
      { stdio: 'ignore' },
    );
    await waitFor(async () => {
      try {
        const res = await fetch(`${base}/healthz`);
        return res.ok;
      } catch {
        return false;
      }
    }, 'server /healthz');

    browser = await chromium.launch();
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });

    const errors = [];
    const badResponses = [];
    const assetStatus = new Map();
    const apiCalls = [];
    const feeds = [...FEEDS];
    const detailGates = new Map();
    const retryDetailPath = '/api/items/102';
    let refreshRunning = false;
    let articleRevision = 0;
    const translationStatus = { running: false, revision: 0 };
    let firstPageGate;
    let readGate;
    let translationStatusGate;
    let translatedSearchGate;
    page.on('console', (msg) => {
      if (msg.type() !== 'error') return;
      if (msg.location().url === `${base}${retryDetailPath}` && msg.text().includes('503')) return;
      errors.push(msg.text());
    });
    page.on('pageerror', (err) => errors.push(String(err)));
    page.on('response', (res) => {
      const { pathname } = new URL(res.url());
      if (res.status() >= 400 && !(res.status() === 503 && pathname === retryDetailPath)) {
        badResponses.push(`${res.status()} ${pathname}`);
      }
      if (pathname.startsWith('/stylesheets/') || pathname.startsWith('/javascripts/')) {
        assetStatus.set(pathname, res.status());
      }
    });

    await page.route('**/api/**', async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      apiCalls.push({ method: req.method(), path, search: url.search });
      const ok = (body) => route.fulfill({
        status: 200, contentType: 'application/json', body: JSON.stringify(body),
      });
      const noContent = () => route.fulfill({ status: 204, body: '' });

      if (path === '/api/status') {
        if (translationStatusGate) await translationStatusGate;
        return ok({ running: refreshRunning, article_revision: articleRevision, version: 'e2e', title_translations: translationStatus });
      }
      if (path === '/api/capabilities') return ok({ title_translation: true });
      if (path === '/api/settings') {
        return req.method() === 'PUT'
          ? noContent()
          : ok({ theme: 'light', font_size: 'medium', refresh_rate: '30', item_max_age_days: '30' });
      }
      if (path === '/api/folders') return ok(FOLDERS);
      if (path === '/api/feeds/find') return ok([]);
      if (path === '/api/feeds') {
        if (req.method() === 'POST') {
          const feed = { id: 3, title: 'New empty feed', feed_url: req.postDataJSON().url };
          feeds.push(feed);
          return ok(feed);
        }
        return ok(feeds);
      }
      if (path === '/api/feeds/refresh' && req.method() === 'POST') {
        refreshRunning = true;
        return route.fulfill({
          status: 202, contentType: 'application/json', body: JSON.stringify({ article_revision: articleRevision }),
        });
      }
      if (path === '/api/feeds/1/title-translations' && req.method() === 'POST') {
        translationStatus.running = true;
        return route.fulfill({
          status: 202, contentType: 'application/json', body: JSON.stringify({ accepted: 4 }),
        });
      }
      if (path === '/api/stats') {
        const unread = {};
        const starred = {};
        for (const item of allItems) {
          if (item.status === 'unread') unread[item.feed_id] = (unread[item.feed_id] || 0) + 1;
          if (item.starred) starred[item.feed_id] = (starred[item.feed_id] || 0) + 1;
        }
        return ok({ unread, starred });
      }
      if (/^\/api\/feeds\/\d+\/icon$/.test(path)) {
        return route.fulfill({ status: 200, contentType: 'image/svg+xml', body: ICON_SVG });
      }
      if (path === '/api/items' && req.method() === 'GET') {
        const q = url.searchParams;
        if (firstPageGate && !q.has('cursor')) await firstPageGate;
        if (translatedSearchGate && q.get('search') && Number(q.get('limit')) > 40) {
          await translatedSearchGate;
        }
        let items = allItems;
        if (q.get('feed_id')) items = items.filter((it) => it.feed_id === Number(q.get('feed_id')));
        if (q.get('folder_id')) items = items.filter((it) => it.feed_id === Number(q.get('folder_id')));
        if (q.get('status') === 'unread') items = items.filter((it) => it.status === 'unread');
        if (q.get('starred') === 'true') items = items.filter((it) => it.starred);
        const search = q.get('search');
        if (search) items = items.filter((it) => it.title.includes(search) || it.translated_title?.includes(search));
        const cursor = Number(q.get('cursor') || 0);
        const offset = Number(q.get('offset') || 0);
        // Fixture IDs increase as dates get older.
        const remaining = items.filter((it) => it.id > cursor).slice(offset);
        const limit = Number(q.get('limit') || 40);
        const nextCursor = remaining.length > limit ? String(remaining[limit - 1].id) : undefined;
        const pageItems = remaining.slice(0, limit);
        return ok({
          items: q.get('include_content') === '0'
            ? pageItems.map(({ content, guid, image, ...summary }) => summary)
            : pageItems,
          next_cursor: nextCursor,
          total: q.get('include_total') === '0' ? undefined : items.length,
          title_translations: search ? translationStatus : undefined,
        });
      }
      if (path === '/api/items/mark-all') return noContent();
      if (/^\/api\/items\/\d+$/.test(path)) {
        const item = allItems.find((it) => it.id === Number(path.split('/').at(-1)));
        if (!item) return route.fulfill({ status: 404, body: '' });
        if (req.method() === 'GET') {
          const gate = detailGates.get(item.id);
          detailGates.delete(item.id);
          const detail = gate ? await gate : item;
          return detail ? ok(detail) : route.fulfill({ status: 503, body: '' });
        }
        if (req.method() === 'PUT') {
          const changes = req.postDataJSON();
          if (readGate && changes.status === 'read') await readGate;
          Object.assign(item, changes);
          return noContent();
        }
      }
      return route.fulfill({ status: 404, body: '' });
    });
    await page.route(`${base}/page?*`, (route) => route.fulfill({
      json: { content: '<article><p>Readability of article 101.</p></article>' },
    }));

    // 1. The login page renders and every bundled asset loads.
    await page.goto(`${base}/login`, { waitUntil: 'networkidle' });
    assert((await page.title()).includes('Hayari'), 'login page title');
    await page.fill('#username', USER);
    await page.fill('#password', PASS);
    await Promise.all([
      page.waitForURL((url) => url.pathname === '/'),
      page.click('button[type=submit]'),
    ]);

    // 2. The app shell renders feeds, folders, and badges. The Unread view
    // hides a folder whose feeds have no unread items.
    await page.waitForSelector('#feed-list .feed-row');
    assert(await page.locator('#feed-list .feed-row').count() === 2, 'sidebar feed rows');
    assert(await page.locator('#feed-list .folder-header').count() === 1, 'unread view hides empty folder');
    assert(await page.locator('.sidebar-count-badge').count() > 0, 'sidebar count badges');
    assert(apiCalls.filter(c => c.path === '/api/stats').length === 1, 'startup requests counts once');

    // The All view brings the empty folder back.
    await page.click('#status-tabs button[data-filter="all"]');
    await waitFor(
      async () => (await page.locator('#feed-list .folder-header').count()) === 2,
      'all view shows the empty folder',
    );

    // 3. Selecting a feed filters the item list (page size 40 of 45 items).
    await page.locator('.feed-row').first().click();
    await waitFor(
      () => apiCalls.some((c) => c.method === 'GET' && c.path === '/api/items' && c.search.includes('feed_id=1')),
      'items request for feed 1',
    );
    await waitFor(
      async () => (await page.locator('#item-list .item-entry').count()) === 40,
      'first page of 40 items',
    );

    // 4. Opening an article loads its body on demand and ignores responses from older selections.
    const detailGets = (id) => apiCalls.filter(c => c.method === 'GET' &&
      (id === undefined ? /^\/api\/items\/\d+$/.test(c.path) : c.path === `/api/items/${id}`));
    assert(detailGets().length === 0, 'listing articles does not fetch their bodies');
    const article = (id) => page.locator(`#item-list [data-item-id="${id}"]`);
    const detailBody = page.locator('#detail-body');
    const waitForDetailResponse = (id) => page.waitForResponse(r =>
      r.request().method() === 'GET' && new URL(r.url()).pathname === `/api/items/${id}`);
    const nextFrames = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    let releaseArticle100;
    let releaseArticle101;
    detailGates.set(100, new Promise(resolve => { releaseArticle100 = resolve; }));
    detailGates.set(101, new Promise(resolve => { releaseArticle101 = resolve; }));
    try {
      await article(100).click();
      await waitFor(() => detailGets(100).length === 1, 'first article body request');
      assert(await article(100).getAttribute('aria-selected') === 'true', 'row is selected while body loads');
      assert((await page.textContent('#detail-title')).includes('Sample article 100'), 'title appears while body loads');
      assert((await page.textContent('#detail-meta')).includes('E2E Author'), 'meta appears while body loads');
      assert((await detailBody.textContent()).includes('Loading'), 'body shows its loading state');

      await page.keyboard.press('j');
      await waitFor(() => detailGets(101).length === 1, 'keyboard opens the next article');
      await page.click('#btn-readability');
      await waitFor(async () => (await detailBody.textContent()).includes('Readability of article 101'), 'Readability while body loads');
      const response101 = waitForDetailResponse(101);
      releaseArticle101(feed1Items[1]);
      await (await response101).finished();
      await nextFrames();
      assert((await detailBody.textContent()).includes('Readability of article 101'), 'feed body does not replace Readability');
      await page.click('#btn-readability');
      await waitFor(async () => (await detailBody.textContent()).includes('Body of article 101'), 'leaving Readability restores the body');

      await page.keyboard.press('k');
      await waitFor(async () => (await detailBody.textContent()).includes('Body of article 100'), 'reselecting the first article loads its body');
      const staleResponse = waitForDetailResponse(100);
      releaseArticle100({ ...feed1Items[0], content: '<p>Stale body</p>' });
      await (await staleResponse).finished();
      await nextFrames();
      assert((await detailBody.textContent()).includes('Body of article 100'), 'older response for the same article is ignored');
      assert(await detailBody.locator('a[href^="https:"]').getAttribute('target') === '_blank', 'external article link opens a new tab');
      assert(await detailBody.locator('a[href^="#"]').getAttribute('target') === null, 'article fragment link stays in the page');
      await article(101).click();
      await waitFor(async () => (await detailBody.textContent()).includes('Body of article 101'), 'reopening a loaded article restores its body');
      assert(detailGets(101).length === 1, 'reopening a loaded row reuses the body');
    } finally {
      releaseArticle100(feed1Items[0]);
      releaseArticle101(feed1Items[1]);
    }

    let releaseClosedArticle;
    detailGates.set(102, new Promise(resolve => { releaseClosedArticle = resolve; }));
    try {
      await article(102).click();
      await waitFor(() => detailGets(102).length === 1, 'body request for article to close');
      await page.keyboard.press('q');
      const closedResponse = waitForDetailResponse(102);
      releaseClosedArticle({ ...feed1Items[2], content: '<p>Closed article body</p>' });
      await (await closedResponse).finished();
      await nextFrames();
      assert(await page.locator('#item-detail-content').isHidden(), 'late response keeps the article closed');
    } finally {
      releaseClosedArticle(feed1Items[2]);
    }
    detailGates.set(102, Promise.resolve(null));
    await article(102).click();
    await detailBody.getByRole('button', { name: 'Retry' }).click();
    await waitFor(async () => (await detailBody.textContent()).includes('Body of article 102'), 'retry recovers a failed body load');
    assert(detailGets(102).length === 3, 'closed response was discarded and failure was retried');
    await article(100).click();

    // 5. Toggling the star issues the API mutation and updates the button.
    const beforeStar = apiCalls.length;
    await page.click('#btn-star');
    await waitFor(
      () => apiCalls.slice(beforeStar).some((c) => c.method === 'PUT' && c.path.startsWith('/api/items/')),
      'star update request',
    );
    await waitFor(
      async () => (await page.locator('#btn-star.active').count()) === 1,
      'star button active state',
    );

    // 6. Search queries the API and filters the list.
    await page.fill('#search-input', 'Sample article 100');
    await waitFor(
      () => apiCalls.some((c) => c.path === '/api/items' && c.search.includes('search=Sample')),
      'search request',
    );
    await waitFor(
      async () => (await page.locator('#item-list .item-entry').count()) >= 1,
      'search results',
    );
    await page.fill('#search-input', '');
    await waitFor(
      async () => (await page.locator('#item-list .item-entry').count()) === 40,
      'list reset after clearing the search',
    );

    // 7. The settings dialog opens with saved values.
    await page.click('#btn-settings');
    await page.waitForSelector('#modal-settings[open]');
    assert(
      await page.inputValue('#modal-settings select[name="theme"]') === 'light',
      'settings theme value',
    );
    await page.keyboard.press('Escape');

    // 8. Infinite scroll loads the remaining items (40 → 45). Scroll away and
    // back so the sentinel's intersection state changes and the observer fires
    // even if it was already visible.
    await page.evaluate(() => {
      document.querySelector('#item-list').scrollTop = 0;
    });
    await page.waitForTimeout(100);
    await page.evaluate(() => {
      const list = document.querySelector('#item-list');
      list.scrollTop = list.scrollHeight;
    });
    await waitFor(
      async () => (await page.locator('#item-list .item-entry').count()) === 45,
      'infinite scroll loads the remaining items',
    );

    // 9. Reading an item in the first page does not skip an unread article in the second.
    for (const item of feed1Items) item.status = 'unread';
    await page.click('#status-tabs button[data-filter="unread"]');
    await waitFor(async () => (await page.locator('#item-list .item-entry.unread').count()) === 40, 'first unread page');
    const beforeRead = apiCalls.length;
    let releaseRead;
    let releaseReadDetail;
    readGate = new Promise(resolve => { releaseRead = resolve; });
    detailGates.set(100, new Promise(resolve => { releaseReadDetail = resolve; }));
    try {
      await page.locator('#item-list .item-entry').first().evaluate(row => {
        row.click();
        row.click();
      });
      await waitFor(() => ['PUT', 'GET'].every(method => apiCalls.slice(beforeRead).some(c =>
        c.method === method && c.path === '/api/items/100')), 'read and body requests start in parallel');
      releaseRead();
      await page.waitForSelector('#btn-toggle-read.active');
      releaseReadDetail({ ...feed1Items[0], status: 'unread', starred: false, title: 'Stale title' });
      await waitFor(async () => (await detailBody.textContent()).includes('Body of article 100'), 'body after mark-read');
      assert(await page.locator('#btn-toggle-read.active').count() === 1, 'body response preserves updated read state');
      assert(await page.locator('#btn-star.active').count() === 1, 'body response preserves current star');
      assert((await page.textContent('#detail-title')).includes('Sample article 100'), 'body response preserves current title');
      assert(apiCalls.slice(beforeRead).filter(c => c.method === 'GET' && c.path === '/api/items/100').length === 1,
        'double-clicking a row shares one body request');
    } finally {
      releaseRead();
      readGate = null;
      releaseReadDetail(feed1Items[0]);
    }
    await waitFor(async () => (await page.locator('#item-list .item-entry.unread').count()) === 39, 'first item marked read');
    await page.evaluate(() => {
      const list = document.querySelector('#item-list');
      list.scrollTop = list.scrollHeight;
    });
    await waitFor(async () => (await page.locator('#item-list .item-entry.unread').count()) === 44, 'remaining unread articles');
    assert(await page.locator('#item-list [data-item-id="140"]').count() === 1, 'first article from the second page');
    assert(apiCalls.slice(beforeRead).filter(c => c.method === 'PUT').length === 1, 'opening twice sends one read request');
    await waitFor(
      async () => await page.locator('[data-feed-id="1"] .sidebar-count-badge').textContent() === '44',
      'reading once decreases the badge once',
    );

    // 10. Articles and unread counts update as soon as a feed saves new articles.
    const beforeRefresh = apiCalls.length;
    await page.click('#btn-refresh');
    await waitFor(
      () => apiCalls.slice(beforeRefresh).filter(c => c.path === '/api/status').length >= 2,
      'refresh status polling',
    );
    assert(!apiCalls.slice(beforeRefresh).some(c => ['/api/items', '/api/stats'].includes(c.path)),
      'polling without new articles skips list and count requests');
    allItems.unshift(makeItem(99, 1));
    articleRevision++;
    await waitFor(
      async () => await page.locator('#item-list .item-entry').first().getAttribute('data-item-id') === '99' &&
        await page.locator('[data-feed-id="1"] .sidebar-count-badge').textContent() === '45',
      'list and unread count update while acquisition is still running',
    );
    assert((await detailBody.textContent()).includes('Body of article 100'), 'refresh preserves the open article');

    allItems.unshift({ ...makeItem(98, 1), status: 'unread' });
    articleRevision++;
    refreshRunning = false;
    await waitFor(async () => await page.locator('#btn-refresh').isEnabled(), 'refresh completes');
    assert(await page.locator('[data-feed-id="1"] .sidebar-count-badge').textContent() === '46', 'badge includes the last article');
    assert(await page.locator('#item-list .item-entry').first().getAttribute('data-item-id') === '98', 'list includes the last article');

    // 11. Selecting a feed again includes articles saved while the page was open.
    allItems.unshift(makeItem(97, 1));
    const beforeSourceReload = apiCalls.length;
    await page.locator('[data-feed-id="1"] .feed-row').click();
    await waitFor(
      () => apiCalls.slice(beforeSourceReload).some(c => c.path === '/api/stats'),
      'reopening the feed refreshes the counts',
    );
    await waitFor(
      async () => await page.locator('[data-feed-id="1"] .sidebar-count-badge').textContent() === '47',
      'badge includes the article saved in the background',
    );
    assert(await page.locator('#item-list .item-entry').first().getAttribute('data-item-id') === '97', 'list includes the background article');

    // 12. A sentinel already inside the margin during page one must still load page two.
    await page.addInitScript(() => {
      const NativeObserver = window.IntersectionObserver;
      window.IntersectionObserver = class extends NativeObserver {
        constructor(callback, options) {
          super((entries, observer) => {
            if (entries.some(entry => entry.isIntersecting)) window.sentinelWasVisible = true;
            callback(entries, observer);
          }, options);
        }
      };
    });
    let releaseFirstPage;
    firstPageGate = new Promise(resolve => { releaseFirstPage = resolve; });
    try {
      await page.setViewportSize({ width: 1280, height: 3600 });
      await page.reload();
      await page.waitForFunction(() => window.sentinelWasVisible);
    } finally {
      releaseFirstPage();
      firstPageGate = null;
    }
    const unreadCount = allItems.filter(item => item.status === 'unread').length;
    await waitFor(
      async () => await page.locator('#item-list .item-entry').count() === unreadCount,
      'infinite scroll fills the tall viewport after a delayed first page',
    );

    // 13. Newly translated search matches above the cursor appear without moving the visible article.
    const translatedMatches = allItems.filter(item => item.feed_id === 1 && item.status === 'unread');
    const newMatches = translatedMatches.slice(0, 4);
    for (const item of translatedMatches.slice(4)) item.title = '検索語 ' + item.title;
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.locator('[data-feed-id="1"] .feed-row').click();
    await page.locator('#search-input').fill('検索語');
    const searchRows = page.locator('#item-list .item-entry');
    await waitFor(
      async () => (await searchRows.count()) === 40 &&
        (await searchRows.first().getAttribute('data-item-id')) === String(translatedMatches[4].id),
      'first page of matches before translation',
    );
    page.once('dialog', dialog => dialog.accept());
    await page.locator('#btn-translate-titles').click();
    await waitFor(() => translationStatus.running, 'search translation started');
    const list = page.locator('#item-list');
    const feedRow = page.locator('[data-feed-id="1"] .feed-row');
    const beforeScrollRow = await feedRow.elementHandle();
    const scrollStats = page.waitForResponse(response => new URL(response.url()).pathname === '/api/stats');
    await list.evaluate(el => { el.scrollTop = el.scrollHeight; });
    await waitFor(async () => (await searchRows.count()) === translatedMatches.length - 4, 'all original matches');
    await (await scrollStats).finished();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert(await feedRow.evaluate((row, previous) => row === previous, beforeScrollRow), 'unchanged counts preserve the sidebar DOM');
    await beforeScrollRow.dispose();
    await list.evaluate(el => { el.scrollTop = el.scrollHeight; });
    const firstVisible = () => list.evaluate(el => {
      const top = el.getBoundingClientRect().top;
      return [...el.children].find(row => row.dataset.itemId && row.getBoundingClientRect().bottom > top)?.dataset.itemId;
    });
    const anchor = await firstVisible();
    for (const item of newMatches) {
      item.translated_title = '検索語の翻訳';
      item.title_translation_state = 'translated';
    }
    translationStatus.revision++;
    translationStatus.running = false;
    await waitFor(async () => (await searchRows.count()) === translatedMatches.length, 'all translated search matches');
    assert(await searchRows.first().getAttribute('data-item-id') === String(newMatches[0].id), 'new matches sort first');
    assert(await firstVisible() === anchor, 'first visible article is preserved');

    // 14. A status response and a subsequent page can observe the same translation revision.
    translationStatus.running = true;
    let releaseStatus;
    let releaseSearch;
    translationStatusGate = new Promise(resolve => { releaseStatus = resolve; });
    translatedSearchGate = new Promise(resolve => { releaseSearch = resolve; });
    try {
      const beforeOverlap = apiCalls.length;
      await feedRow.click();
      await waitFor(async () => (await searchRows.count()) === 40, 'fresh first search page');
      await waitFor(() => apiCalls.slice(beforeOverlap).some(c => c.path === '/api/status'), 'translation status pending');
      translationStatus.revision++;
      translationStatus.running = false;
      await list.evaluate(el => { el.scrollTop = el.scrollHeight; });
      const searchReloads = () => apiCalls.slice(beforeOverlap).filter(c =>
        c.path === '/api/items' && Number(new URLSearchParams(c.search).get('limit')) > 40);
      await waitFor(() => searchReloads().length > 0, 'scroll detects the new revision');

      const statusResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/status');
      releaseStatus();
      await (await statusResponse).finished();
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      releaseSearch();
      await waitFor(async () => (await searchRows.count()) === translatedMatches.length, 'all matches once');
      assert(searchReloads().length === 1, 'status and paging share a single search reload');
    } finally {
      releaseStatus();
      releaseSearch();
      translationStatusGate = null;
      translatedSearchGate = null;
    }

    // 15. Subscribing to an empty feed selects it without fetching the previous scope.
    await page.locator('#search-input').fill('');
    await waitFor(async () => (await searchRows.count()) === 40, 'cleared search');
    const beforeSubscribe = apiCalls.length;
    await page.click('#btn-add-feed');
    await page.locator('#input-feed-url').fill('https://example.invalid/empty.xml');
    await page.click('#btn-feed-submit');
    await waitFor(async () =>
      await page.locator('#item-list-title').textContent() === 'New empty feed' &&
      await page.locator('#item-list-empty').isVisible(), 'new feed selected and empty');
    const subscribeCalls = apiCalls.slice(beforeSubscribe);
    const subscribeItems = subscribeCalls.filter(c => c.path === '/api/items');
    assert(subscribeItems.length === 1 && new URLSearchParams(subscribeItems[0].search).get('feed_id') === '3', 'subscribe requests only the new feed');
    assert(subscribeCalls.filter(c => c.path === '/api/stats').length === 1, 'subscribe requests counts once');
    assert(await page.locator('#status-tabs button[data-filter="all"]').evaluate(el => el.classList.contains('active')), 'empty subscription is shown in All');

    // 16. No broken assets and no console errors.
    assert(apiCalls.filter(c => c.path === '/api/items').every(c => new URLSearchParams(c.search).get('include_total') === '0'), 'Web UI skips the full result count');
    assert(apiCalls.filter(c => c.path === '/api/items').every(c => new URLSearchParams(c.search).get('include_content') === '0'), 'Web UI requests summary rows');
    for (const asset of ASSETS) {
      assert(assetStatus.get(asset) === 200, `${asset} → ${assetStatus.get(asset)}`);
    }
    assert(badResponses.length === 0, `failed requests: ${badResponses.join(' | ')}`);
    assert(errors.length === 0, `console errors: ${errors.join(' | ')}`);

    console.log('browser-e2e: PASS');
  } finally {
    if (browser) await browser.close().catch(() => {});
    if (server) {
      server.kill('SIGTERM');
      await new Promise((resolve) => {
        server.once('exit', resolve);
        setTimeout(resolve, 2000);
      });
    }
    rmSync(tmp, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error(`browser-e2e: FAIL — ${err.message}`);
  process.exit(1);
});
