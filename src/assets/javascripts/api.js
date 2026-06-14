// api.js — Low-level API helpers

const API = (() => {
  async function request(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    if (res.status === 401) {
      window.location.href = '/login';
      return null;
    }
    if (!res.ok) {
      throw new Error(`${method} ${path} → ${res.status}`);
    }
    if (res.status === 204) return null;
    return res.json();
  }

  // Folders
  const getFolders    = ()          => request('GET',    '/api/folders');
  const createFolder  = (title)     => request('POST',   '/api/folders', { title });
  const updateFolder  = (id, data)  => request('PUT',    `/api/folders/${id}`, data);
  const deleteFolder  = (id)        => request('DELETE', `/api/folders/${id}`);

  // Feeds
  const getFeeds      = ()          => request('GET',    '/api/feeds');
  const createFeed    = (url, fid)  => request('POST',   '/api/feeds', { url, folder_id: fid });
  const updateFeed    = (id, data)  => request('PUT',    `/api/feeds/${id}`, data);
  const deleteFeed    = (id)        => request('DELETE', `/api/feeds/${id}`);
  const findFeeds     = (url)       => request('GET',    `/api/feeds/find?url=${encodeURIComponent(url)}`);
  const refreshFeeds  = ()          => request('POST',   '/api/feeds/refresh');
  const getFeedErrors = ()          => request('GET',    '/api/feeds/errors');

  // Items
  const getItems = (params) => {
    const qs = new URLSearchParams(
      Object.fromEntries(Object.entries(params).filter(([, v]) => v != null && v !== ''))
    ).toString();
    return request('GET', `/api/items${qs ? '?' + qs : ''}`);
  };
  const updateItem  = (id, data)  => request('PUT',  `/api/items/${id}`, data);
  const markAllRead = (params)    => {
    const qs = new URLSearchParams(
      Object.fromEntries(Object.entries(params || {}).filter(([, v]) => v != null))
    ).toString();
    return request('POST', `/api/items/mark-all${qs ? '?' + qs : ''}`);
  };

  // Stats (per-feed unread counts)
  const getStats = () => request('GET', '/api/stats');

  // Settings
  const getSettings  = ()     => request('GET', '/api/settings');
  const saveSettings = (data) => request('PUT', '/api/settings', data);

  // Page (readability)
  const fetchPage = (url) => request('GET', `/page?url=${encodeURIComponent(url)}`);

  // OPML
  async function importOPML(xmlBody) {
    const res = await fetch('/opml/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/xml' },
      body: xmlBody,
    });
    if (!res.ok) throw new Error(`OPML import → ${res.status}`);
    return res.json();
  }
  const exportOPML = () => { window.location.href = '/opml/export'; };

  return {
    getFolders, createFolder, updateFolder, deleteFolder,
    getFeeds, createFeed, updateFeed, deleteFeed, findFeeds, refreshFeeds, getFeedErrors,
    getItems, updateItem, markAllRead,
    getStats,
    getSettings, saveSettings,
    fetchPage,
    importOPML, exportOPML,
  };
})();
