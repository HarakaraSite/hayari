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
    content: `<p>Body of article ${id}. Here is a <a href="https://example.invalid/">link</a>.</p>`,
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
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', (err) => errors.push(String(err)));
    page.on('response', (res) => {
      const { pathname } = new URL(res.url());
      if (res.status() >= 400) badResponses.push(`${res.status()} ${pathname}`);
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

      if (path === '/api/status') return ok({ running: true, version: 'e2e' });
      if (path === '/api/capabilities') return ok({ title_translation: false });
      if (path === '/api/settings') {
        return req.method() === 'PUT'
          ? noContent()
          : ok({ theme: 'light', font_size: 'medium', refresh_rate: '30', item_max_age_days: '30' });
      }
      if (path === '/api/folders') return ok(FOLDERS);
      if (path === '/api/feeds') return ok(FEEDS);
      if (path === '/api/stats') return ok({ unread: { 1: 23, 2: 1 }, starred: { 1: 1 } });
      if (/^\/api\/feeds\/\d+\/icon$/.test(path)) {
        return route.fulfill({ status: 200, contentType: 'image/svg+xml', body: ICON_SVG });
      }
      if (path === '/api/items' && req.method() === 'GET') {
        const q = url.searchParams;
        let items = allItems;
        if (q.get('feed_id')) items = items.filter((it) => it.feed_id === Number(q.get('feed_id')));
        if (q.get('folder_id')) items = items.filter((it) => it.feed_id === Number(q.get('folder_id')));
        if (q.get('status') === 'unread') items = items.filter((it) => it.status === 'unread');
        if (q.get('starred') === 'true') items = items.filter((it) => it.starred);
        if (q.get('search')) items = items.filter((it) => it.title.includes(q.get('search')));
        const offset = Number(q.get('offset') || 0);
        const limit = Number(q.get('limit') || 40);
        return ok({ items: items.slice(offset, offset + limit), total: items.length });
      }
      if (path === '/api/items/mark-all') return noContent();
      if (path.startsWith('/api/items/')) return noContent();
      return route.fulfill({ status: 404, body: '' });
    });

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

    // 4. Opening an article shows title, meta, and body.
    await page.locator('#item-list .item-entry').first().click();
    await page.waitForSelector('#item-detail-content:not([hidden])');
    assert((await page.textContent('#detail-title')).includes('Sample article'), 'detail title');
    assert((await page.textContent('#detail-body')).includes('link'), 'detail body');

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

    // 9. No broken assets and no console errors.
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
