// app.js — yarr2 main application (vanilla JS)
'use strict';

const App = (() => {

  // ── State ──────────────────────────────────────────────────────
  const state = {
    feeds:       [],
    folders:     [],
    feedStats:   {},     // feedId(string) → unread count
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
    addFeedMode: 'find',   // 'find' | 'subscribe'
  };

  const PAGE_SIZE = 40;

  // ── DOM refs ───────────────────────────────────────────────────
  const $ = (sel) => document.querySelector(sel);

  let appEl,
      feedList, itemList, itemListTitle, itemListEmpty,
      btnRefresh, btnMarkAllRead, btnAddFeed, btnSettings,
      searchInput,
      modalAddFeed, formAddFeed, inputFeedUrl, feedCandidates, btnFeedSubmit,
      modalManage, modalManageTitle, modalManageBody,
      modalSettings, formSettings,
      btnOPMLImport, btnOPMLExport, inputOPMLFile,
      detailEmpty, detailContent,
      detailTitle, detailMeta, detailBody,
      btnStar, btnToggleRead, btnReadability, btnOpen, btnPrevItem, btnNextItem,
      loadMoreSentinel;

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
    btnAddFeed       = $('#btn-add-feed');
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

    await Promise.all([loadSidebar(), loadSettings()]);
    setupEventListeners();
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
      const folderUnread = folderFeeds.reduce((sum, f) => sum + (state.feedStats[f.id] || 0), 0);
      const isActive = state.sourceType === 'folder' && state.sourceId === folder.id;
      const isOpen   = folder.is_expanded;

      const li = document.createElement('li');
      li.dataset.folderId = folder.id;

      // Folder header row
      const header = document.createElement('div');
      header.className = 'folder-header' + (isActive ? ' active' : '');
      header.innerHTML =
        `<span class="folder-toggle${isOpen ? ' open' : ''}">&#x25b6;</span>` +
        `<span class="folder-name">${escHTML(folder.title)}</span>` +
        (folderUnread ? `<span class="unread-badge">${folderUnread}</span>` : '') +
        `<button class="feed-action-btn" data-action="edit-folder" data-id="${folder.id}" title="Edit">&#x22ef;</button>`;

      // Folder feed list (collapsible)
      const feedsUl = document.createElement('ul');
      feedsUl.className = 'folder-feeds';
      feedsUl.hidden = !isOpen;
      folderFeeds.forEach(feed => {
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
    unfoldered.forEach(feed => {
      feedList.appendChild(makeFeedLi(feed));
    });

    updateActiveSidebarItem();
  }

  function makeFeedLi(feed) {
    const unread = state.feedStats[feed.id] || 0;
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
    row.insertAdjacentHTML('beforeend',
      `<span class="feed-name">${escHTML(feed.title || feed.feed_url)}</span>` +
      (unread ? `<span class="unread-badge">${unread}</span>` : '') +
      `<button class="feed-action-btn" data-action="edit-feed" data-id="${feed.id}" title="Edit">&#x22ef;</button>`
    );

    row.addEventListener('click', (e) => {
      if (e.target.dataset.action) return;
      selectSource('feed', feed.id, feed.title || feed.feed_url);
    });

    li.appendChild(row);
    return li;
  }

  // Update unread badges for one feed (and its folder) without rebuilding
  // the sidebar DOM — a full renderSidebar() per article open causes churn
  // and favicon flicker.
  function refreshBadges(feedId) {
    setBadge(
      feedList.querySelector(`li[data-feed-id="${feedId}"] .feed-row`),
      state.feedStats[feedId] || 0
    );
    const feed = state.feeds.find(f => f.id === feedId);
    if (feed && feed.folder_id) {
      const sum = state.feeds
        .filter(f => f.folder_id === feed.folder_id)
        .reduce((acc, f) => acc + (state.feedStats[f.id] || 0), 0);
      setBadge(
        feedList.querySelector(`li[data-folder-id="${feed.folder_id}"] .folder-header`),
        sum
      );
    }
  }

  function setBadge(row, count) {
    if (!row) return;
    let badge = row.querySelector('.unread-badge');
    if (count > 0) {
      if (!badge) {
        badge = document.createElement('span');
        badge.className = 'unread-badge';
        row.insertBefore(badge, row.querySelector('.feed-action-btn'));
      }
      badge.textContent = count;
    } else if (badge) {
      badge.remove();
    }
  }

  function updateActiveSidebarItem() {
    feedList.querySelectorAll('.feed-row, .folder-header').forEach(el => {
      const feedId   = el.closest('[data-feed-id]')?.dataset.feedId;
      const folderId = el.closest('[data-folder-id]')?.dataset.folderId;
      let active = false;
      if (state.sourceType === 'feed'   && feedId   && +feedId   === state.sourceId) active = true;
      if (state.sourceType === 'folder' && folderId && +folderId === state.sourceId) active = true;
      el.classList.toggle('active', active);
    });
  }

  function selectSource(type, id, name) {
    state.sourceType = type;
    state.sourceId   = id;
    itemListTitle.textContent = name || 'All items';
    updateActiveSidebarItem();
    appEl.classList.remove('show-sidebar'); // mobile: back to the list pane
    loadItems(true);
  }

  function selectFilter(filter) {
    state.filter = filter;
    // Update tab highlight
    document.querySelectorAll('#status-tabs button').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.filter === filter);
    });
    // Update title when no specific source selected
    if (state.sourceType === 'all') {
      const labels = { unread: 'Unread', starred: 'Starred', all: 'All items' };
      itemListTitle.textContent = labels[filter] || 'Items';
    }
    // Show mark-all-read only in unread view
    btnMarkAllRead.hidden = (filter !== 'unread');
    appEl.classList.remove('show-sidebar'); // mobile: back to the list pane
    loadItems(true);
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
      item.status = 'read';
      if (li) li.classList.remove('unread');
      API.updateItem(item.id, { status: 'read' });
      // Update stats badge
      if (state.feedStats[item.feed_id] > 0) {
        state.feedStats[item.feed_id]--;
        refreshBadges(item.feed_id);
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
    detailBody.scrollTop = 0;
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
    await API.updateItem(item.id, { starred: newStarred });

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
  }

  async function toggleRead() {
    const item = state.selectedItem;
    if (!item) return;
    const newStatus = item.status === 'read' ? 'unread' : 'read';
    await setItemStatus(item, newStatus);
  }

  async function setItemStatus(item, status) {
    const old = item.status;
    item.status = status;
    await API.updateItem(item.id, { status });

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
      refreshBadges(item.feed_id);
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
      await API.createFeed(url, folderID);
      modalAddFeed.close();
      await loadSidebar();
      await loadItems(true);
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
      await API.updateFeed(feedId, payload);
      modalManage.close();
      await loadSidebar();
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

  // ── Source navigation (l / h) ──────────────────────────────────
  function buildSourceList() {
    const list = [{ type: 'all', id: null, name: 'All' }];
    state.folders.forEach(folder => {
      list.push({ type: 'folder', id: folder.id, name: folder.title });
      state.feeds.filter(f => f.folder_id === folder.id).forEach(feed => {
        list.push({ type: 'feed', id: feed.id, name: feed.title || feed.feed_url });
      });
    });
    state.feeds.filter(f => !f.folder_id).forEach(feed => {
      list.push({ type: 'feed', id: feed.id, name: feed.title || feed.feed_url });
    });
    return list;
  }

  function navigateSource(dir) {
    const list = buildSourceList();
    const idx  = list.findIndex(s => s.type === state.sourceType && s.id === state.sourceId);
    const next = list[Math.max(0, Math.min(list.length - 1, idx + dir))];
    selectSource(next.type, next.id, next.name);
  }

  // ── Mark all read ──────────────────────────────────────────────
  async function markAllRead() {
    const params = {};
    if (state.sourceType === 'feed')   params.feed_id   = state.sourceId;
    if (state.sourceType === 'folder') params.folder_id = state.sourceId;
    await API.markAllRead(params);
    // Refresh stats and items
    const stats = await API.getStats();
    state.feedStats = (stats && stats.unread) || {};
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
        if (el && s[key] != null) el.value = s[key];
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
      } else {
        document.documentElement.setAttribute('data-theme', s.theme);
      }
    }
    const sizes = { small: '0.9rem', medium: '1rem', large: '1.15rem' };
    if (s.font_size && sizes[s.font_size]) {
      document.documentElement.style.setProperty('--article-font-size', sizes[s.font_size]);
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
      await API.refreshFeeds();
      await Promise.all([loadSidebar(), loadItems(true)]);
      btnRefresh.removeAttribute('aria-busy');
    });

    // Mark all read
    btnMarkAllRead.addEventListener('click', markAllRead);

    // Search
    searchInput.addEventListener('input', handleSearchInput);
    searchInput.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { searchInput.blur(); searchInput.value = ''; state.search = ''; loadItems(true); }
    });

    // Add feed
    btnAddFeed.addEventListener('click', openAddFeedModal);
    formAddFeed.addEventListener('submit', handleAddFeedSubmit);

    // Feed/folder action button delegation
    feedList.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-action]');
      if (!btn) return;
      e.stopPropagation();
      const { action, id } = btn.dataset;
      if (action === 'edit-feed')   openEditFeed(id);
      if (action === 'edit-folder') openEditFolder(id);
    });

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
