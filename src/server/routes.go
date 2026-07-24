package server

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nkanaev/yarr2/src/assets"
	"github.com/nkanaev/yarr2/src/content"
	"github.com/nkanaev/yarr2/src/safehttp"
	"github.com/nkanaev/yarr2/src/storage"
	"github.com/nkanaev/yarr2/src/worker"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	fs := http.FileServer(http.FS(assets.FS))

	auth := s.authMiddleware

	// Static assets (including index.html) are auth-protected so the app
	// shell is not exposed without credentials. They are embedded in the
	// executable, so retaining an old copy after a local restart would run a
	// mismatched application. /login stays public.
	mux.HandleFunc("/", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" {
			http.ServeFileFS(w, r, assets.FS, "index.html")
			return
		}
		fs.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/login", s.handleWebLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/healthz", s.handleHealthz)

	mux.HandleFunc("/api/status", auth(s.handleStatus))

	// Folders
	mux.HandleFunc("/api/folders", auth(s.handleFolders))
	mux.HandleFunc("/api/folders/", auth(s.handleFolder))

	// Feeds — more specific paths first (ServeMux gives priority to longer patterns)
	mux.HandleFunc("/api/feeds", auth(s.handleFeeds))
	mux.HandleFunc("/api/feeds/errors", auth(s.handleFeedErrors))
	mux.HandleFunc("/api/feeds/find", auth(s.handleFeedFind))
	mux.HandleFunc("/api/feeds/refresh", auth(s.handleFeedsRefresh))
	mux.HandleFunc("/api/feeds/", auth(s.handleFeed)) // catches /api/feeds/:id and /api/feeds/:id/icon

	// Items
	mux.HandleFunc("/api/items", auth(s.handleItems))
	mux.HandleFunc("/api/items/mark-all", auth(s.handleMarkAllRead))
	mux.HandleFunc("/api/items/", auth(s.handleItem))

	// Filters
	mux.HandleFunc("/api/filters", auth(s.handleFilters))
	mux.HandleFunc("/api/filters/", auth(s.handleFilter))

	// Settings
	mux.HandleFunc("/api/settings", auth(s.handleSettings))

	// Stats (per-feed unread counts)
	mux.HandleFunc("/api/stats", auth(s.handleStats))

	// OPML
	mux.HandleFunc("/opml/import", auth(s.handleOPMLImport))
	mux.HandleFunc("/opml/export", auth(s.handleOPMLExport))

	// Article full-text fetch
	mux.HandleFunc("/page", auth(s.handlePage))
}

var pageClient = safehttp.NewClient(15 * time.Second)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"running": s.worker.Running(),
		"version": s.Version,
	})
}

// handleHealthz reports whether the process can reach its SQLite database.
// It intentionally stays unauthenticated so it can be used by liveness probes.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.db.Ping(); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// --- Folders ---

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folders, err := s.db.ListFolders()
		if err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, folders)
	case http.MethodPost:
		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		folder, err := s.db.CreateFolder(body.Title)
		if err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, folder)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFolder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r.URL.Path, "/api/folders/")
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Title      string `json:"title"`
			IsExpanded *bool  `json:"is_expanded"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		if body.Title != "" {
			if err := s.db.UpdateFolder(id, body.Title); err != nil {
				httpError(w, err, 500)
				return
			}
		}
		if body.IsExpanded != nil {
			if err := s.db.UpdateFolderExpanded(id, *body.IsExpanded); err != nil {
				httpError(w, err, 500)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.db.DeleteFolder(id); err != nil {
			httpError(w, err, 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Feeds ---

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		feeds, err := s.db.ListFeeds()
		if err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, feeds)
	case http.MethodPost:
		var body struct {
			URL      string `json:"url"`
			FolderID *int64 `json:"folder_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		feed, err := s.db.CreateFeed(body.URL, body.FolderID)
		if err != nil {
			httpError(w, err, 500)
			return
		}
		go s.worker.RefreshFeed(feed.ID)
		writeJSON(w, feed)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Dispatch icon sub-resource: /api/feeds/:id/icon
	if strings.HasSuffix(path, "/icon") {
		id, err := pathID(strings.TrimSuffix(path, "/icon"), "/api/feeds/")
		if err != nil {
			http.Error(w, "invalid id", 400)
			return
		}
		s.handleFeedIcon(w, r, id)
		return
	}

	id, err := pathID(path, "/api/feeds/")
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		// folder_id uses RawMessage to distinguish "key absent" (keep current
		// folder) from explicit null (move out of folder).
		var body struct {
			Title    *string         `json:"title"`
			FolderID json.RawMessage `json:"folder_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		if body.Title != nil {
			if err := s.db.UpdateFeed(id, body.Title, nil); err != nil {
				httpError(w, err, 500)
				return
			}
		}
		if len(body.FolderID) > 0 {
			var folderID *int64
			if err := json.Unmarshal(body.FolderID, &folderID); err != nil {
				httpError(w, err, 400)
				return
			}
			if err := s.db.UpdateFeedFolder(id, folderID); err != nil {
				httpError(w, err, 500)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.db.DeleteFeed(id); err != nil {
			httpError(w, err, 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFeedErrors returns a map of feed_id → error string for feeds with errors.
func (s *Server) handleFeedErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	errors, err := s.db.ListFeedErrors()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, errors)
}

// handleFeedIcon serves the favicon stored as a base64 data URL with ETag caching.
func (s *Server) handleFeedIcon(w http.ResponseWriter, r *http.Request, feedID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	feed, err := s.db.GetFeed(feedID)
	if err != nil || feed == nil || feed.Icon == nil {
		http.NotFound(w, r)
		return
	}

	dataURL := *feed.Icon
	// Expected format: data:<mime>;base64,<encoded>
	if !strings.HasPrefix(dataURL, "data:") {
		http.NotFound(w, r)
		return
	}
	rest := dataURL[5:]
	semiIdx := strings.Index(rest, ";")
	if semiIdx < 0 || !strings.HasPrefix(rest[semiIdx+1:], "base64,") {
		http.NotFound(w, r)
		return
	}
	mimeType := rest[:semiIdx]
	encoded := rest[semiIdx+8:] // skip ";base64,"

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "invalid icon data", http.StatusInternalServerError)
		return
	}

	sum := md5.Sum(data)
	etag := fmt.Sprintf(`"%x"`, sum)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleFeedFind(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "url required", 400)
		return
	}
	feeds, err := worker.FindFeeds(url)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, feeds)
}

func (s *Server) handleFeedsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go s.worker.RefreshAll()
	w.WriteHeader(http.StatusAccepted)
}

// --- Items ---

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	filter := storage.ItemFilter{
		Status:   q.Get("status"),
		Starred:  parseBoolPtr(q.Get("starred")),
		FeedID:   parseIntPtr(q.Get("feed_id")),
		FolderID: parseIntPtr(q.Get("folder_id")),
		Search:   q.Get("search"),
	}
	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	items, err := s.db.ListItems(filter, limit, offset)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	total, err := s.db.CountItems(filter)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r.URL.Path, "/api/items/")
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Status  *string `json:"status"`
		Starred *bool   `json:"starred"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, err, 400)
		return
	}
	if body.Status != nil {
		if *body.Status != "read" && *body.Status != "unread" {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		if err := s.db.UpdateItemStatus(id, *body.Status); err != nil {
			httpError(w, err, 500)
			return
		}
	}
	if body.Starred != nil {
		if err := s.db.SetStarred(id, *body.Starred); err != nil {
			httpError(w, err, 500)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	filter := storage.ItemFilter{
		FeedID:   parseIntPtr(q.Get("feed_id")),
		FolderID: parseIntPtr(q.Get("folder_id")),
	}
	if err := s.db.MarkAllRead(filter); err != nil {
		httpError(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Filters ---

func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		filters, err := s.db.ListFilters()
		if err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, filters)
	case http.MethodPost:
		var body storage.Filter
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		filter, err := s.db.CreateFilter(body)
		if err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, filter)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFilter(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r.URL.Path, "/api/filters/")
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body storage.Filter
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		body.ID = id
		if err := s.db.UpdateFilter(body); err != nil {
			httpError(w, err, 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.db.DeleteFilter(id); err != nil {
			httpError(w, err, 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Settings ---

// clientSettings is the whitelist of keys exposed via the settings API.
// Internal keys (auth_secret) must never be readable or writable by clients.
var clientSettings = map[string]bool{
	"theme":        true,
	"font_size":    true,
	"refresh_rate": true,
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.db.GetAllSettings()
		if err != nil {
			httpError(w, err, 500)
			return
		}
		filtered := make(map[string]string, len(clientSettings))
		for k, v := range settings {
			if clientSettings[k] {
				filtered[k] = v
			}
		}
		writeJSON(w, filtered)
	case http.MethodPut:
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err, 400)
			return
		}
		for k, v := range body {
			if !clientSettings[k] {
				http.Error(w, "unknown setting: "+k, http.StatusBadRequest)
				return
			}
			if err := s.db.SetSetting(k, v); err != nil {
				httpError(w, err, 500)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Stats ---

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stats, err := s.db.GetUnreadCountsByFeed()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	unread := make(map[int64]int64, len(stats))
	for _, st := range stats {
		unread[st.FeedID] = st.Count
	}
	writeJSON(w, map[string]interface{}{"unread": unread})
}

// --- Page (article fetch for Readability mode) ---

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pageURL := r.URL.Query().Get("url")
	if pageURL == "" {
		http.Error(w, "url required", 400)
		return
	}

	target, err := url.Parse(pageURL)
	if err != nil || safehttp.ValidateURL(target) != nil {
		http.Error(w, "unsafe URL", http.StatusBadRequest)
		return
	}
	resp, err := pageClient.Get(pageURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, map[string]string{"content": content.PageHTML(string(body))})
}

// --- Helpers ---

func pathID(path, prefix string) (int64, error) {
	s := path[len(prefix):]
	return strconv.ParseInt(s, 10, 64)
}

func parseIntPtr(s string) *int64 {
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func parseBoolPtr(s string) *bool {
	if s == "" {
		return nil
	}
	v := s == "true" || s == "1"
	return &v
}
