// app.js — hayari main application (vanilla JS)
'use strict';

const App = (() => {

  // ── State ──────────────────────────────────────────────────────
  const state = {
    feeds:       [],
    folders:     [],
    feedStats:   {},     // feedId(string) → unread count
    starStats:   {},     // feedId(string) → starred item count
    filter:      'unread', // 'unread' | 'starred' | 'all'
    sourceType:  'all',   // 'all' | 'feed' | 'folder'
    sourceId:    null,
    search:      '',
    items:       [],
    total:       0,
    offset:      0,
    loading:     false,
    selectedItem: null,
    readabilityMode: false,
    unreadListNeedsRefresh: false,
    addFeedMode: 'find',   // 'find' | 'subscribe'
  };

  const PAGE_SIZE = 40;
  const MAX_UNREAD_BADGE_COUNT = 999;

  // ── DOM refs ───────────────────────────────────────────────────
  const $ = (sel) => document.querySelector(sel);

  let appEl,
      feedList, itemList, itemListTitle, itemListEmpty,
      btnRefresh, btnMarkAllRead, btnManageSource, btnAddFeed, btnAddFolder, btnSettings,
      searchInput,
      modalAddFeed, formAddFeed, inputFeedUrl, feedCandidates, btnFeedSubmit,
      modalManage, modalManageTitle, modalManageBody,
      modalSettings, formSettings,
      btnOPMLImport, btnOPMLExport, inputOPMLFile,
      detailEmpty, detailContent,
      detailTitle, detailMeta, detailBody,
      btnStar, btnToggleRead, btnReadability, btnOpen, btnPrevItem, btnNextItem,
      loadMoreSentinel, sidebarResizer, itemListResizer;

  const columnWidths = {
    sidebar: { cssVar: '--sidebar-width', storageKey: 'hayari.v2.sidebar-width', fallback: 288, min: 200 },
    itemList: { cssVar: '--item-list-width', storageKey: 'hayari.v2.item-list-width', fallback: 320, min: 240 },
  };
  const minDetailWidth = 280;
  const resizerWidth = 16;
  let preferredColumnWidths;

  // ── Init ───────────────────────────────────────────────────────
  async function init() {
    // Wire DOM refs
    appEl            = $('#app');
    feedList         = $('#feed-list');
    itemList         = $('#item-list');
    itemListTitle    = $('#item-list-title');
    itemListEmpty    = $('#item-list-empty');
    btnRefresh       = $('#btn-refresh');
    btnMarkAllRead   = $('#btn-mark-all-read');
    btnManageSource  = $('#btn-manage-source');
    btnAddFeed       = $('#btn-add-feed');
    btnAddFolder     = $('#btn-add-folder');
    btnSettings      = $('#btn-settings');
    searchInput      = $('#search-input');
    modalAddFeed     = $('#modal-add-feed');
    formAddFeed      = $('#form-add-feed');
    inputFeedUrl     = $('#input-feed-url');
    feedCandidates   = $('#feed-candidates');
    btnFeedSubmit    = $('#btn-feed-submit');
    modalManage      = $('#modal-manage');
    modalManageTitle = $('#modal-manage-title');
    modalManageBody  = $('#modal-manage-body');
    modalSettings    = $('#modal-settings');
    formSettings     = $('#form-settings');
    btnOPMLImport    = $('#btn-opml-import');
    btnOPMLExport    = $('#btn-opml-export');
    inputOPMLFile    = $('#input-opml-file');
    detailEmpty      = $('#item-detail-empty');
    detailContent    = $('#item-detail-content');
    detailTitle      = $('#detail-title');
    detailMeta       = $('#detail-meta');
    detailBody       = $('#detail-body');
    btnStar          = $('#btn-star');
    btnToggleRead    = $('#btn-toggle-read');
    btnReadability   = $('#btn-readability');
    btnOpen          = $('#btn-open');
    btnPrevItem      = $('#btn-prev-item');
    btnNextItem      = $('#btn-next-item');
    loadMoreSentinel = $('#load-more-sentinel');
    sidebarResizer   = $('#sidebar-resizer');
    itemListResizer  = $('#item-list-resizer');

    await Promise.all([loadSidebar(), loadSettings()]);
    setupEventListeners();
    setupColumnResizers();
    setupInfiniteScroll();
    setupKeyBindings();
    await loadItems(true);
  }

  // ── Sidebar ────────────────────────────────────────────────────
  async function loadSidebar() {
    const [feeds, folders, stats] = await Promise.all([
      API.getFeeds(), API.getFolders(), API.getStats(),
    ]);
    state.feeds    = feeds   || [];
    state.folders  = folders || [];
    state.feedStats = (stats && stats.unread) || {};
    state.starStats = (stats && stats.starred) || {};
    renderSidebar();
  }

  function renderSidebar() {
    feedList.innerHTML = '';

    // Build folder map
    const byFolder = {};
    state.folders.forEach(f => { byFolder[f.id] = []; });
    const unfoldered = [];
    state.feeds.forEach(feed => {
      if (feed.folder_id && byFolder[feed.folder_id]) {
        byFolder[feed.folder_id].push(feed);
      } else {
        unfoldered.push(feed);
      }
    });

    // Render folders
    state.folders.forEach(folder => {
      const folderFeeds = byFolder[folder.id] || [];
      const visibleFolderFeeds = visibleSidebarFeeds(folderFeeds);
      if ((state.filter === 'unread' || state.filter === 'starred') && visibleFolderFeeds.length === 0) return;
      const folderCount = sidebarCount(folderFeeds);
      const isActive = state.sourceType === 'folder' && state.sourceId === folder.id;
      const isOpen   = folder.is_expanded;

      const li = document.createElement('li');
      li.dataset.folderId = folder.id;

      // Folder header row
      const header = document.createElement('div');
      header.className = 'folder-header' + (isActive ? ' active' : '');
      header.innerHTML =
        `<span class="folder-toggle${isOpen ? ' open' : ''}">&#x25b6;</span>` +
        `<span class="folder-name">${escHTML(folder.title)}</span>`;
      appendCountBadge(header, folderCount);

      // Folder feed list (collapsible)
      const feedsUl = document.createElement('ul');
      feedsUl.className = 'folder-feeds';
      feedsUl.hidden = !isOpen;
      visibleFolderFeeds.forEach(feed => {
        feedsUl.appendChild(makeFeedLi(feed));
      });

      // Triangle click toggles open/closed; clicking the rest of the header
      // selects the folder as source (so expanding doesn't force a reload)
      header.addEventListener('click', (e) => {
        if (e.target.dataset.action) return; // handled by delegation
        if (e.target.classList.contains('folder-toggle')) {
          const nowOpen = feedsUl.hidden;
          feedsUl.hidden = !nowOpen;
          e.target.classList.toggle('open', nowOpen);
          folder.is_expanded = nowOpen;
          API.updateFolder(folder.id, { is_expanded: nowOpen });
          return;
        }
        selectSource('folder', folder.id, folder.title);
      });

      li.appendChild(header);
      li.appendChild(feedsUl);
      feedList.appendChild(li);
    });

    // Render unfoldered feeds
    visibleSidebarFeeds(unfoldered).forEach(feed => {
      feedList.appendChild(makeFeedLi(feed));
    });

    updateActiveSidebarItem();
  }

  function visibleSidebarFeeds(feeds) {
    if (state.filter === 'unread') {
      return feeds.filter(feed => (state.feedStats[feed.id] || 0) > 0);
    }
    if (state.filter === 'starred') {
      return feeds.filter(feed => (state.starStats[feed.id] || 0) > 0);
    }
    return feeds;
  }

  function sidebarCount(feeds) {
    if (state.filter === 'unread') {
      return feeds.reduce((sum, feed) => sum + (state.feedStats[feed.id] || 0), 0);
    }
    if (state.filter === 'starred') {
      return feeds.reduce((sum, feed) => sum + (state.starStats[feed.id] || 0), 0);
    }
    return 0;
  }

  function sidebarFeedCount(feed) {
    if (state.filter === 'unread') return state.feedStats[feed.id] || 0;
    if (state.filter === 'starred') return state.starStats[feed.id] || 0;
    return 0;
  }

  function ensureVisibleSource() {
    if (state.filter !== 'unread' && state.filter !== 'starred') return false;

    const stats = state.filter === 'unread' ? state.feedStats : state.starStats;

    let hidden = false;
    if (state.sourceType === 'feed') {
      const feed = state.feeds.find(f => f.id === state.sourceId);
      hidden = !feed || (stats[feed.id] || 0) === 0;
    } else if (state.sourceType === 'folder') {
      const count = state.feeds
        .filter(f => f.folder_id === state.sourceId)
        .reduce((sum, f) => sum + (stats[f.id] || 0), 0);
      hidden = count === 0;
    }
    if (!hidden) return false;

    state.sourceType = 'all';
    state.sourceId = null;
    itemListTitle.textContent = state.filter === 'unread' ? 'Unread' : 'Starred';
    btnManageSource.hidden = true;
    return true;
  }

  function makeFeedLi(feed) {
    const count = sidebarFeedCount(feed);
    const isActive = state.sourceType === 'feed' && state.sourceId === feed.id;

    const li = document.createElement('li');
    li.dataset.feedId = feed.id;

    const row = document.createElement('div');
    row.className = 'feed-row' + (isActive ? ' active' : '');

    // Favicon
    const img = document.createElement('img');
    img.className = 'feed-icon';
    img.src = `/api/feeds/${feed.id}/icon`;
    img.width = 16; img.height = 16;
    img.alt = '';
    img.onerror = () => { img.style.display = 'none'; };

    row.appendChild(img);
    row.insertAdjacentHTML('beforeend', `<span class="feed-name">${escHTML(feed.title || feed.feed_url)}</span>`);
    appendCountBadge(row, count);

    row.addEventListener('click', () => selectSource('feed', feed.id, feed.title || feed.feed_url));

    li.appendChild(row);
    return li;
  }

  // Reading an item keeps it visible until the user returns to the source list.
  // The counts still update immediately; the next source navigation applies the
  // unread filter and removes feeds whose counts reached zero.
  function refreshBadges() {
    if (state.filter === 'unread') {
      state.unreadListNeedsRefresh = true;
      syncUnreadSidebarCounts();
    }
  }

  function appendCountBadge(row, count) {
    setCountBadge(row, count);
  }

  function setCountBadge(row, count, { showZero = false } = {}) {
    let badge = row.querySelector('.sidebar-count-badge');
    if (count <= 0 && !showZero) {
      badge?.remove();
      return;
    }
    if (!badge) {
      badge = document.createElement('span');
      badge.className = 'sidebar-count-badge';
      row.appendChild(badge);
    }
    updateCountBadge(badge, count);
  }

  function syncUnreadSidebarCounts() {
    feedList.querySelectorAll('[data-feed-id]').forEach(li => {
      const feedID = +li.dataset.feedId;
      const row = li.querySelector('.feed-row');
      if (row) setCountBadge(row, state.feedStats[feedID] || 0, { showZero: true });
    });
    feedList.querySelectorAll('[data-folder-id]').forEach(li => {
      const folderID = +li.dataset.folderId;
      const row = li.querySelector('.folder-header');
      if (!row) return;
      const count = state.feeds
        .filter(feed => feed.folder_id === folderID)
        .reduce((sum, feed) => sum + (state.feedStats[feed.id] || 0), 0);
      setCountBadge(row, count, { showZero: true });
    });
  }

  function applyUnreadListRefresh() {
    if (state.filter !== 'unread' || !state.unreadListNeedsRefresh) return;
    state.unreadListNeedsRefresh = false;
    ensureVisibleSource();
    renderSidebar();
  }

  function updateCountBadge(badge, count) {
    badge.textContent = count > MAX_UNREAD_BADGE_COUNT ? `${MAX_UNREAD_BADGE_COUNT}+` : String(count);
    const label = state.filter === 'starred' ? 'starred items' : 'unread items';
    badge.setAttribute('aria-label', `${count} ${label}`);
    badge.title = `${count} ${label}`;
  }

  function updateActiveSidebarItem() {
    feedList.querySelectorAll('.feed-row').forEach(row => {
      const feedID = +row.closest('[data-feed-id]').dataset.feedId;
      row.classList.toggle('active', state.sourceType === 'feed' && feedID === state.sourceId);
    });
    feedList.querySelectorAll('.folder-header').forEach(header => {
      const folderID = +header.closest('[data-folder-id]').dataset.folderId;
      header.classList.toggle('active', state.sourceType === 'folder' && folderID === state.sourceId);
    });
  }

  function selectSource(type, id, name) {
    applyUnreadListRefresh();
    state.sourceType = type;
    state.sourceId   = id;
    itemListTitle.textContent = name || 'All items';
    btnManageSource.hidden = (type === 'all');
    updateActiveSidebarItem();
    appEl.classList.remove('show-sidebar'); // mobile: back to the list pane
    return loadItems(true);
  }

  function selectFilter(filter) {
    applyUnreadListRefresh();
    state.filter = filter;
    ensureVisibleSource();
    // Update tab highlight
    document.querySelectorAll('#status-tabs button').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.filter === filter);
    });
    // Update title when no specific source selected
    if (state.sourceType === 'all') {
      const labels = { unread: 'Unread', starred: 'Starred', all: 'All items' };
      itemListTitle.textContent = labels[filter] || 'Items';
    }
    btnManageSource.hidden = (state.sourceType === 'all');
    // Show mark-all-read only in unread view
    btnMarkAllRead.hidden = (filter !== 'unread');
    appEl.classList.remove('show-sidebar'); // mobile: back to the list pane
    renderSidebar();
    return loadItems(true);
  }

  // ── Item list ──────────────────────────────────────────────────
  async function loadItems(reset = true) {
    if (state.loading) return;
    state.loading = true;

    if (reset) {
      state.items  = [];
      state.offset = 0;
      itemList.innerHTML = '';
      itemListEmpty.hidden = true;
    }

    const params = buildItemParams();
    params.limit  = PAGE_SIZE;
    params.offset = state.offset;

    try {
      const data     = await API.getItems(params);
      const newItems = (data && data.items) || [];
      state.total    = (data && data.total) || 0;
      state.items    = [...state.items, ...newItems];
      state.offset  += newItems.length;
      renderItems(newItems);
      itemListEmpty.textContent = 'No items';
      itemListEmpty.hidden = state.items.length > 0;
    } catch (err) {
      console.error('loadItems:', err);
      itemListEmpty.textContent = 'Failed to load items';
      itemListEmpty.hidden = false;
    } finally {
      state.loading = false;
    }
  }

  function buildItemParams() {
    const p = {};
    if (state.filter === 'starred') {
      p.starred = 'true';
    } else if (state.filter !== 'all') {
      p.status = state.filter;
    }
    if (state.sourceType === 'feed')   p.feed_id   = state.sourceId;
    if (state.sourceType === 'folder') p.folder_id = state.sourceId;
    if (state.search) p.search = state.search;
    return p;
  }

  function renderItems(items) {
    const feedMap = {};
    state.feeds.forEach(f => { feedMap[f.id] = f.title || f.feed_url; });

    items.forEach(item => {
      const li = document.createElement('li');
      li.dataset.itemId = item.id;
      li.setAttribute('role', 'option');
      li.setAttribute('aria-selected', 'false');
      const isUnread  = item.status === 'unread';
      const isStarred = !!item.starred;
      li.className = 'item-entry' +
        (isUnread  ? ' unread'  : '') +
        (isStarred ? ' starred' : '');

      li.innerHTML =
        `<div class="item-top-row">` +
          `<span class="item-feed-name">${escHTML(feedMap[item.feed_id] || '')}</span>` +
          (isStarred ? `<span class="item-star-mark">&#x2605;</span>` : '') +
          `<span class="item-date">${relativeDate(item.date)}</span>` +
        `</div>` +
        `<span class="item-title">${escHTML(itemTitle(item))}</span>`;

      li.addEventListener('click', () => selectItem(item));
      itemList.appendChild(li);
    });
  }

  // ── Item selection ─────────────────────────────────────────────
  async function selectItem(item) {
    // Deselect previous
    itemList.querySelectorAll('.selected').forEach(el => {
      el.classList.remove('selected');
      el.setAttribute('aria-selected', 'false');
    });

    // Select current
    const li = itemList.querySelector(`[data-item-id="${item.id}"]`);
    if (li) {
      li.classList.add('selected');
      li.setAttribute('aria-selected', 'true');
      li.scrollIntoView({ block: 'nearest' });
    }

    state.selectedItem    = item;
    state.readabilityMode = false;

    // Mark read on open
    if (item.status === 'unread') {
      try {
        await API.updateItem(item.id, { status: 'read' });
      } catch (err) {
        console.error('mark item read:', err);
        showDetail(item);
        return;
      }
      item.status = 'read';
      if (li) li.classList.remove('unread');
      // Update stats badge
      if (state.feedStats[item.feed_id] > 0) {
        state.feedStats[item.feed_id]--;
        refreshBadges();
      }
    }

    showDetail(item);
  }

  function currentIndex() {
    if (!state.selectedItem) return -1;
    return state.items.findIndex(i => i.id === state.selectedItem.id);
  }

  async function navigateItems(dir) {
    const items = state.items;
    if (!items.length) return;
    const idx    = currentIndex();
    const newIdx = Math.max(0, Math.min(items.length - 1, idx + dir));

    // Auto-load more if near end
    if (newIdx >= items.length - 5 && items.length < state.total) {
      await loadItems(false);
    }

    if (newIdx !== idx || idx === -1) {
      selectItem(state.items[newIdx]);
    }
  }

  // ── Article detail ─────────────────────────────────────────────
  function showDetail(item) {
    detailEmpty.hidden   = true;
    detailContent.hidden = false;
    appEl.classList.add('show-detail');     // narrow screens: detail pane takes over
    appEl.classList.remove('show-sidebar');
    btnReadability.classList.remove('active');

    // Title
    detailTitle.textContent = itemTitle(item);

    // Toolbar open link
    btnOpen.href = item.link || '#';

    // Toolbar states
    updateDetailToolbar(item);

    // Meta
    const feedName = state.feeds.find(f => f.id === item.feed_id);
    const parts = [];
    if (feedName) parts.push(`<a href="#" data-source-feed="${item.feed_id}">${escHTML(feedName.title || feedName.feed_url)}</a>`);
    if (item.author) parts.push(escHTML(item.author));
    if (item.date)   parts.push(`<time>${fullDate(item.date)}</time>`);
    if (item.link)   parts.push(`<a href="${encHTML(item.link)}" target="_blank" rel="noopener">Original</a>`);
    detailMeta.innerHTML = parts.join('<span class="sep"> · </span>');
    // Feed-name clicks are handled by the delegated listener in setupEventListeners

    // Body
    detailBody.innerHTML = item.content || '<p><em>No content.</em></p>';
    prepareArticleLinks(detailBody);
    detailBody.scrollTop = 0;
  }

  function prepareArticleLinks(container) {
    container.querySelectorAll('a[href]').forEach(link => {
      if (link.getAttribute('href').startsWith('#')) return;
      const rel = new Set(link.rel.split(/\s+/).filter(Boolean));
      rel.add('noopener');
      rel.add('noreferrer');
      link.target = '_blank';
      link.rel = [...rel].join(' ');
    });
  }

  function updateDetailToolbar(item) {
    if (!item) return;
    btnStar.textContent       = item.starred ? '★' : '☆';
    btnStar.classList.toggle('active', !!item.starred);
    btnToggleRead.textContent = item.status === 'read' ? '●' : '○';
    btnToggleRead.classList.toggle('active', item.status === 'read');
  }

  async function toggleStar() {
    const item = state.selectedItem;
    if (!item) return;
    const newStarred = !item.starred;
    item.starred = newStarred;

    try {
      await API.updateItem(item.id, { starred: newStarred });
    } catch (_) {
      // Rollback on failure
      item.starred = !newStarred;
      updateDetailToolbar(item);
      return;
    }

    const li = itemList.querySelector(`[data-item-id="${item.id}"]`);
    if (li) {
      li.classList.toggle('starred', newStarred);
      const mark = li.querySelector('.item-star-mark');
      if (newStarred && !mark) {
        const span = document.createElement('span');
        span.className = 'item-star-mark';
        span.innerHTML = '&#x2605;';
        li.querySelector('.item-top-row').insertBefore(span, li.querySelector('.item-date'));
      } else if (!newStarred && mark) {
        mark.remove();
      }
    }

    updateDetailToolbar(item);
    state.starStats[item.feed_id] = Math.max(0, (state.starStats[item.feed_id] || 0) + (newStarred ? 1 : -1));

    if (state.filter === 'starred') {
      ensureVisibleSource();
      renderSidebar();
      await loadItems(true);
    }
  }

  async function toggleRead() {
    const item = state.selectedItem;
    if (!item) return;
    const newStatus = item.status === 'read' ? 'unread' : 'read';
    await setItemStatus(item, newStatus);
  }

  async function setItemStatus(item, status) {
    const old = item.status;
    if (old === status) return;
    try {
      await API.updateItem(item.id, { status });
    } catch (err) {
      console.error('set item status:', err);
      return;
    }
    item.status = status;

    // Update list item CSS
    const li = itemList.querySelector(`[data-item-id="${item.id}"]`);
    if (li) {
      li.classList.toggle('unread', status === 'unread');
    }

    // Update toolbar
    updateDetailToolbar(item);

    // Adjust badge counts
    const delta = (status === 'unread' ? 1 : 0) - (old === 'unread' ? 1 : 0);
    if (delta !== 0) {
      state.feedStats[item.feed_id] = Math.max(0, (state.feedStats[item.feed_id] || 0) + delta);
      refreshBadges();
    }
  }

  async function toggleReadability() {
    if (!state.selectedItem) return;
    state.readabilityMode = !state.readabilityMode;
    btnReadability.classList.toggle('active', state.readabilityMode);

    if (state.readabilityMode) {
      detailBody.innerHTML = '<p><em>Loading&#x2026;</em></p>';
      try {
        const result = await API.fetchPage(state.selectedItem.link);
        if (result && result.content) {
          detailBody.innerHTML = extractContent(result.content);
        } else {
          throw new Error('empty');
        }
      } catch (_) {
        detailBody.innerHTML = '<p><em>Could not fetch article.</em></p>';
        state.readabilityMode = false;
        btnReadability.classList.remove('active');
      }
    } else {
      detailBody.innerHTML = state.selectedItem.content || '<p><em>No content.</em></p>';
    }
    detailBody.scrollTop = 0;
  }

  // Simple client-side content extraction
  function extractContent(html) {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    ['script','style','noscript','nav','header','footer','aside','form','iframe'].forEach(tag => {
      doc.querySelectorAll(tag).forEach(el => el.remove());
    });
    const main = doc.querySelector('main, article, [role="main"], .entry-content, .post-content, #content');
    return (main || doc.body || doc.documentElement).innerHTML;
  }

  function openCurrentExternal() {
    if (state.selectedItem && state.selectedItem.link) {
      window.open(state.selectedItem.link, '_blank', 'noopener');
    }
  }

  function closeDetail() {
    state.selectedItem = null;
    itemList.querySelectorAll('.selected').forEach(el => {
      el.classList.remove('selected');
      el.setAttribute('aria-selected', 'false');
    });
    detailEmpty.hidden   = false;
    detailContent.hidden = true;
    appEl.classList.remove('show-detail');
  }

  function scrollDetail(dy) {
    detailBody.scrollBy({ top: dy, behavior: 'smooth' });
  }

  // ── Feed management ────────────────────────────────────────────
  function openAddFeedModal() {
    inputFeedUrl.value    = '';
    feedCandidates.hidden = true;
    feedCandidates.innerHTML = '';
    btnFeedSubmit.textContent = 'Find';
    state.addFeedMode = 'find';
    modalAddFeed.showModal();
    setTimeout(() => inputFeedUrl.focus(), 50);
  }

  async function handleAddFeedSubmit(e) {
    e.preventDefault();

    if (state.addFeedMode === 'subscribe') {
      // Subscribe to selected candidate or entered URL
      const radio = feedCandidates.querySelector('input[type="radio"]:checked');
      const url   = radio ? radio.value : inputFeedUrl.value.trim();
      await subscribeFeed(url, null);
      return;
    }

    // mode === 'find'
    const url = inputFeedUrl.value.trim();
    if (!url) return;
    btnFeedSubmit.setAttribute('aria-busy', 'true');
    try {
      const candidates = await API.findFeeds(url);
      if (!candidates || candidates.length === 0) {
        // Try direct subscribe
        await subscribeFeed(url, null);
        return;
      }
      if (candidates.length === 1) {
        await subscribeFeed(candidates[0].url, null);
        return;
      }
      // Show candidates
      feedCandidates.innerHTML = candidates.map((c, i) =>
        `<label class="feed-candidate">
          <input type="radio" name="feed-url" value="${encHTML(c.url)}"${i === 0 ? ' checked' : ''}>
          <span>
            <span class="feed-candidate-title">${escHTML(c.title || c.url)}</span>
            <span class="feed-candidate-url">${escHTML(c.url)}</span>
          </span>
        </label>`
      ).join('');
      feedCandidates.hidden = false;
      state.addFeedMode = 'subscribe';
      btnFeedSubmit.textContent = 'Subscribe';
    } catch (err) {
      alert('Error: ' + err.message);
    } finally {
      btnFeedSubmit.removeAttribute('aria-busy');
    }
  }

  async function subscribeFeed(url, folderID) {
    btnFeedSubmit.setAttribute('aria-busy', 'true');
    try {
      const feed = await API.createFeed(url, folderID);
      modalAddFeed.close();
      await loadSidebar();
		if ((state.filter === 'unread' && (state.feedStats[feed.id] || 0) === 0) ||
		    (state.filter === 'starred' && (state.starStats[feed.id] || 0) === 0)) {
		  await selectFilter('all');
		}
		selectSource('feed', feed.id, feed.title || feed.feed_url);
    } catch (err) {
      alert('Error: ' + err.message);
    } finally {
      btnFeedSubmit.removeAttribute('aria-busy');
    }
  }

  function openEditFeed(feedId) {
    const feed = state.feeds.find(f => f.id === +feedId);
    if (!feed) return;

    modalManageTitle.textContent = 'Edit Feed';

    const folderOptions = state.folders.map(f =>
      `<option value="${f.id}"${feed.folder_id === f.id ? ' selected' : ''}>${escHTML(f.title)}</option>`
    ).join('');

    modalManageBody.innerHTML =
      `<form id="form-edit-feed">
        <label>Name
          <input type="text" name="title" value="${encHTML(feed.title || '')}" />
        </label>
        <label>Folder
          <select name="folder_id">
            <option value=""${!feed.folder_id ? ' selected' : ''}>No folder</option>
            ${folderOptions}
          </select>
        </label>
		<label>Hide articles with title keywords
		  <input type="text" name="title_filter_keywords" value="${encHTML(feed.title_filter_keywords || '')}" />
		  <small>Comma-separated keywords. Articles whose titles contain any keyword are hidden when fetched.</small>
		</label>
        <div class="manage-footer">
          <button type="button" class="btn-danger" id="btn-do-delete-feed">Delete</button>
          <button type="submit">Save</button>
        </div>
      </form>`;

    modalManageBody.querySelector('#form-edit-feed').addEventListener('submit', async (e) => {
      e.preventDefault();
      const data = Object.fromEntries(new FormData(e.target));
      const payload = {};
      if (data.title) payload.title = data.title;
      payload.folder_id = data.folder_id ? +data.folder_id : null;
		payload.title_filter_keywords = data.title_filter_keywords;
      await API.updateFeed(feedId, payload);
      modalManage.close();
      await loadSidebar();
		await loadItems(true);
    });

    modalManageBody.querySelector('#btn-do-delete-feed').addEventListener('click', async () => {
      if (!confirm(`Delete "${feed.title || feed.feed_url}"?`)) return;
      await API.deleteFeed(feedId);
      modalManage.close();
      await loadSidebar();
      await loadItems(true);
    });

    modalManage.showModal();
  }

  function openEditFolder(folderId) {
    const folder = state.folders.find(f => f.id === +folderId);
    if (!folder) return;

    modalManageTitle.textContent = 'Edit Folder';
    modalManageBody.innerHTML =
      `<form id="form-edit-folder">
        <label>Name
          <input type="text" name="title" value="${encHTML(folder.title)}" required />
        </label>
        <div class="manage-footer">
          <button type="button" class="btn-danger" id="btn-do-delete-folder">Delete</button>
          <button type="submit">Save</button>
        </div>
      </form>`;

    modalManageBody.querySelector('#form-edit-folder').addEventListener('submit', async (e) => {
      e.preventDefault();
      const { title } = Object.fromEntries(new FormData(e.target));
      await API.updateFolder(folderId, { title });
      modalManage.close();
      await loadSidebar();
    });

    modalManageBody.querySelector('#btn-do-delete-folder').addEventListener('click', async () => {
      if (!confirm(`Delete folder "${folder.title}"? (Feeds will become unfoldered)`)) return;
      await API.deleteFolder(folderId);
      modalManage.close();
      await loadSidebar();
    });

    modalManage.showModal();
  }

  function openCreateFolder() {
    modalManageTitle.textContent = 'New Folder';
    modalManageBody.innerHTML =
      `<form id="form-create-folder">
        <label>Name
          <input type="text" name="title" required autofocus />
        </label>
        <div class="manage-footer">
          <span></span>
          <button type="submit">Create</button>
        </div>
      </form>`;

    const form = modalManageBody.querySelector('#form-create-folder');
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const { title } = Object.fromEntries(new FormData(e.target));
      const cleanTitle = title.trim();
      if (!cleanTitle) return;
      await API.createFolder(cleanTitle);
      modalManage.close();
      await loadSidebar();
    });
    modalManage.showModal();
    form.elements.title.focus();
  }

  // ── Source navigation (l / h) ──────────────────────────────────
  function buildSourceList() {
    const list = [{ type: 'all', id: null, name: 'All' }];
    state.folders.forEach(folder => {
      const folderFeeds = state.feeds.filter(f => f.folder_id === folder.id);
      const visibleFolderFeeds = visibleSidebarFeeds(folderFeeds);
      if ((state.filter === 'unread' || state.filter === 'starred') && visibleFolderFeeds.length === 0) return;
      list.push({ type: 'folder', id: folder.id, name: folder.title });
      visibleFolderFeeds.forEach(feed => {
        list.push({ type: 'feed', id: feed.id, name: feed.title || feed.feed_url });
      });
    });
    visibleSidebarFeeds(state.feeds.filter(f => !f.folder_id)).forEach(feed => {
      list.push({ type: 'feed', id: feed.id, name: feed.title || feed.feed_url });
    });
    return list;
  }

  function navigateSource(dir) {
    applyUnreadListRefresh();
    const list = buildSourceList();
    const idx  = list.findIndex(s => s.type === state.sourceType && s.id === state.sourceId);
    const next = list[Math.max(0, Math.min(list.length - 1, idx + dir))];
    selectSource(next.type, next.id, next.name);
  }

  // ── Mark all read ──────────────────────────────────────────────
  async function markAllRead() {
    if (!window.confirm('Mark all items in this view as read?')) return;

    const params = {};
    if (state.sourceType === 'feed')   params.feed_id   = state.sourceId;
    if (state.sourceType === 'folder') params.folder_id = state.sourceId;
    await API.markAllRead(params);
    // Refresh stats and items
    const stats = await API.getStats();
    state.feedStats = (stats && stats.unread) || {};
    state.starStats = (stats && stats.starred) || {};
    ensureVisibleSource();
    renderSidebar();
    await loadItems(true);
  }

  // ── Search ─────────────────────────────────────────────────────
  let searchTimer = null;

  function handleSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      state.search = searchInput.value.trim();
      loadItems(true);
    }, 300);
  }

  function focusSearch() {
    searchInput.focus();
    searchInput.select();
  }

  // ── Settings ───────────────────────────────────────────────────
  async function loadSettings() {
    const s = await API.getSettings();
    if (s) applySettings(s);
    return s;
  }

  async function openSettings() {
    const s = await API.getSettings();
    if (s) {
      ['theme', 'font_size', 'refresh_rate'].forEach(key => {
        const el = formSettings.querySelector(`[name="${key}"]`);
        if (el && s[key] != null) el.value = key === 'theme' && s[key] === 'beige' ? 'light' : s[key];
      });
    }
    modalSettings.showModal();
  }

  function applySettings(s) {
    if (s.theme) {
      // Pico CSS follows the system preference only when data-theme is absent;
      // "auto" is not a recognized value and would lock the page to light mode.
      if (s.theme === 'auto') {
        document.documentElement.removeAttribute('data-theme');
        document.documentElement.removeAttribute('data-color-scheme');
      } else if (s.theme === 'beige') {
        // Beige was an experimental setting. Keep old saved values readable
        // while the theme is intentionally removed from Settings.
        document.documentElement.setAttribute('data-theme', 'light');
        document.documentElement.removeAttribute('data-color-scheme');
      } else {
        document.documentElement.setAttribute('data-theme', s.theme);
        document.documentElement.removeAttribute('data-color-scheme');
      }
    }
    const sizes = { small: '90%', medium: '100%', large: '115%' };
    if (s.font_size && sizes[s.font_size]) {
      document.documentElement.style.fontSize = sizes[s.font_size];
    }
  }

  // ── OPML ───────────────────────────────────────────────────────
  function triggerOPMLImport() { inputOPMLFile.click(); }

  async function handleOPMLFile(e) {
    const file = e.target.files[0];
    if (!file) return;
    const text = await file.text();
    try {
      const result = await API.importOPML(text);
      alert(`Imported ${result.imported} feed(s).`);
      await loadSidebar();
      await loadItems(true);
    } catch (err) {
      alert('Import failed: ' + err.message);
    }
    inputOPMLFile.value = '';
  }

  // ── Event listeners ────────────────────────────────────────────
  function setupEventListeners() {
    // Status filter tabs
    document.querySelectorAll('#status-tabs button').forEach(btn => {
      btn.addEventListener('click', () => selectFilter(btn.dataset.filter));
    });

    // Refresh
    btnRefresh.addEventListener('click', async () => {
      btnRefresh.setAttribute('aria-busy', 'true');
      btnRefresh.disabled = true;
      try {
        await API.refreshFeeds();
        await Promise.all([loadSidebar(), loadItems(true)]);
      } catch (err) {
        alert('Refresh failed: ' + err.message);
      } finally {
        btnRefresh.removeAttribute('aria-busy');
        btnRefresh.disabled = false;
      }
    });

    // Mark all read
    btnMarkAllRead.addEventListener('click', markAllRead);
    btnManageSource.addEventListener('click', () => {
      if (state.sourceType === 'feed') openEditFeed(state.sourceId);
      if (state.sourceType === 'folder') openEditFolder(state.sourceId);
    });

    // Search
    searchInput.addEventListener('input', handleSearchInput);
    searchInput.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { searchInput.blur(); searchInput.value = ''; state.search = ''; loadItems(true); }
    });

    // Add feed
    btnAddFeed.addEventListener('click', openAddFeedModal);
    btnAddFolder.addEventListener('click', openCreateFolder);
    formAddFeed.addEventListener('submit', handleAddFeedSubmit);

    // Mobile pane navigation
    $('#btn-show-sidebar').addEventListener('click', () => {
      appEl.classList.toggle('show-sidebar');
    });
    $('#btn-back-list').addEventListener('click', () => {
      // Back to the list keeping the selection (q clears it entirely)
      appEl.classList.remove('show-detail');
    });

    // Article toolbar
    btnStar.addEventListener('click', toggleStar);
    btnToggleRead.addEventListener('click', toggleRead);
    btnReadability.addEventListener('click', toggleReadability);
    btnPrevItem.addEventListener('click', () => navigateItems(-1));
    btnNextItem.addEventListener('click', () => navigateItems(1));

    // Meta feed-name click
    detailMeta.addEventListener('click', (e) => {
      const a = e.target.closest('[data-source-feed]');
      if (a) {
        e.preventDefault();
        const fid  = +a.dataset.sourceFeed;
        const feed = state.feeds.find(f => f.id === fid);
        if (feed) selectSource('feed', fid, feed.title || feed.feed_url);
      }
    });

    // Settings
    btnSettings.addEventListener('click', openSettings);
    formSettings.addEventListener('submit', async (e) => {
      e.preventDefault();
      const data = Object.fromEntries(new FormData(formSettings));
      await API.saveSettings(data);
      applySettings(data);
      modalSettings.close();
    });

    // OPML
    btnOPMLImport.addEventListener('click', triggerOPMLImport);
    btnOPMLExport.addEventListener('click', API.exportOPML);
    inputOPMLFile.addEventListener('change', handleOPMLFile);

    // Close modals on backdrop click or data-close-modal buttons
    [modalAddFeed, modalManage, modalSettings].forEach(modal => {
      modal.addEventListener('click', (e) => { if (e.target === modal) modal.close(); });
      modal.querySelectorAll('[data-close-modal]').forEach(btn => {
        btn.addEventListener('click', () => modal.close());
      });
    });

    // Hide mark-all-read on non-unread filter
    btnMarkAllRead.hidden = (state.filter !== 'unread');
  }

  // ── Desktop column resizing ───────────────────────────────────
  function setupColumnResizers() {
    preferredColumnWidths = savedColumnWidths();
    applyColumnWidths(preferredColumnWidths);

    enableColumnResizer(sidebarResizer, 'sidebar', 'itemList');
    enableColumnResizer(itemListResizer, 'itemList', 'sidebar');
    window.addEventListener('resize', () => applyColumnWidths(preferredColumnWidths));
  }

  function savedColumnWidths() {
    return Object.fromEntries(Object.entries(columnWidths).map(([name, column]) => {
      const saved = Number(localStorage.getItem(column.storageKey));
      return [name, Number.isFinite(saved) && saved > 0 ? saved : column.fallback];
    }));
  }

  function applyColumnWidths(widths) {
    if (window.matchMedia('(max-width: 900px)').matches) return;

    const available = appEl.clientWidth - minDetailWidth - resizerWidth;
    let sidebar = Math.max(columnWidths.sidebar.min, widths.sidebar);
    let itemList = Math.max(columnWidths.itemList.min, widths.itemList);
    let overflow = sidebar + itemList - available;

    if (overflow > 0) {
      const itemListReduction = Math.min(overflow, itemList - columnWidths.itemList.min);
      itemList -= itemListReduction;
      overflow -= itemListReduction;
      sidebar = Math.max(columnWidths.sidebar.min, sidebar - overflow);
    }

    appEl.style.setProperty(columnWidths.sidebar.cssVar, `${sidebar}px`);
    appEl.style.setProperty(columnWidths.itemList.cssVar, `${itemList}px`);
  }

  function enableColumnResizer(resizer, columnName, otherColumnName) {
    const column = columnWidths[columnName];
    const otherColumn = columnWidths[otherColumnName];
    resizer.addEventListener('pointerdown', (downEvent) => {
      if (downEvent.button !== 0) return;
      downEvent.preventDefault();

      const startX = downEvent.clientX;
      const startWidth = columnWidth(column);
      const otherWidth = columnWidth(otherColumn);
      const maxWidth = Math.max(column.min, appEl.clientWidth - otherWidth - minDetailWidth - resizerWidth);
      resizer.setPointerCapture(downEvent.pointerId);
      document.body.classList.add('is-resizing-columns');

      const resize = (moveEvent) => {
        if (moveEvent.pointerId !== downEvent.pointerId) return;
        const width = Math.min(maxWidth, Math.max(column.min, startWidth + moveEvent.clientX - startX));
        preferredColumnWidths = { ...preferredColumnWidths, [columnName]: width };
        applyColumnWidths(preferredColumnWidths);
      };
      const finish = (upEvent) => {
        if (upEvent.pointerId !== downEvent.pointerId) return;
        localStorage.setItem(column.storageKey, String(columnWidth(column)));
        document.body.classList.remove('is-resizing-columns');
        document.removeEventListener('pointermove', resize);
        document.removeEventListener('pointerup', finish);
        document.removeEventListener('pointercancel', finish);
      };
      document.addEventListener('pointermove', resize);
      document.addEventListener('pointerup', finish);
      document.addEventListener('pointercancel', finish);
    });
  }

  function columnWidth(column) {
    const value = Number.parseFloat(getComputedStyle(appEl).getPropertyValue(column.cssVar));
    return Number.isFinite(value) ? value : column.fallback;
  }

  // ── Keyboard shortcuts ─────────────────────────────────────────
  function setupKeyBindings() {
    Keys.bind('j', () => navigateItems(1));
    Keys.bind('k', () => navigateItems(-1));
    Keys.bind('l', () => navigateSource(1));
    Keys.bind('h', () => navigateSource(-1));
    Keys.bind('o', openCurrentExternal);
    Keys.bind('r', toggleRead);
    Keys.bind('s', toggleStar);
    Keys.bind('i', toggleReadability);
    Keys.bind('q', closeDetail);
    Keys.bind('f', () => scrollDetail(200));
    Keys.bind('b', () => scrollDetail(-200));
    Keys.bind('/', focusSearch);
    Keys.bind('R', markAllRead);     // shift+R → e.key = 'R'
    Keys.bind('1', () => selectFilter('unread'));
    Keys.bind('2', () => selectFilter('starred'));
    Keys.bind('3', () => selectFilter('all'));
  }

  // ── Infinite scroll ────────────────────────────────────────────
  function setupInfiniteScroll() {
    const observer = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && !state.loading && state.items.length < state.total) {
        loadItems(false);
      }
    }, { threshold: 0.1 });
    observer.observe(loadMoreSentinel);
  }

  // ── Helpers ────────────────────────────────────────────────────
  function itemTitle(item) {
    return item.title || '(no title)';
  }

  function escHTML(str) {
    return String(str ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function encHTML(str) {
    return String(str ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function relativeDate(dateStr) {
    if (!dateStr) return '';
    const d    = new Date(dateStr);
    if (isNaN(d)) return dateStr;
    const diff = (Date.now() - d.getTime()) / 1000; // seconds
    if (diff < 300)        return 'now';
    if (diff < 3600)       return `${Math.floor(diff / 60)}m`;
    if (diff < 86400 * 3)  return `${Math.floor(diff / 3600)}h`;
    if (diff < 86400 * 30) return `${Math.floor(diff / 86400)}d`;
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }

  function fullDate(dateStr) {
    if (!dateStr) return '';
    const d = new Date(dateStr);
    return isNaN(d) ? dateStr : d.toLocaleString(undefined, {
      month: 'short', day: 'numeric', year: 'numeric',
      hour: 'numeric', minute: '2-digit',
    });
  }

  return { init };
})();

document.addEventListener('DOMContentLoaded', () => App.init());
