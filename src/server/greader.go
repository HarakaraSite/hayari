package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

// In-memory token store (token -> expiry). Tokens are lost on restart;
// GReader clients re-login on 401, so this only costs one extra round-trip.
const greaderTokenTTL = 30 * 24 * time.Hour

// FreshRSS exposes its Google Reader-compatible API below this prefix.
// Keep the original Google Reader routes as well for clients that use them.
const freshRSSGReaderPrefix = "/api/greader.php"

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) registerGReaderRoutes(mux *http.ServeMux) {
	s.registerGReaderHandlers(mux)

	// FreshRSS clients send the same Google Reader requests after
	// /api/greader.php. Strip only that prefix, then dispatch to the exact same
	// handlers so both URL forms stay behaviorally identical.
	freshRSSMux := http.NewServeMux()
	s.registerGReaderHandlers(freshRSSMux)
	mux.Handle(freshRSSGReaderPrefix+"/", http.StripPrefix(freshRSSGReaderPrefix, freshRSSMux))
}

func (s *Server) registerGReaderHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/accounts/ClientLogin", s.greaderLogin)

	gr := s.greaderAuthMiddleware

	mux.HandleFunc("/reader/api/0/token", gr(s.greaderToken))
	mux.HandleFunc("/reader/api/0/user-info", gr(s.greaderUserInfo))
	mux.HandleFunc("/reader/api/0/subscription/list", gr(s.greaderSubscriptionList))
	mux.HandleFunc("/reader/api/0/subscription/edit", gr(s.greaderSubscriptionEdit))
	mux.HandleFunc("/reader/api/0/subscription/quickadd", gr(s.greaderSubscriptionQuickAdd))
	mux.HandleFunc("/reader/api/0/subscription/import", gr(s.greaderSubscriptionImport))
	mux.HandleFunc("/reader/api/0/unread-count", gr(s.greaderUnreadCount))
	mux.HandleFunc("/reader/api/0/tag/list", gr(s.greaderTagList))
	mux.HandleFunc("/reader/api/0/stream/contents", gr(s.greaderStreamContents))
	mux.HandleFunc("/reader/api/0/stream/contents/", gr(s.greaderStreamContents))
	mux.HandleFunc("/reader/api/0/stream/items/ids", gr(s.greaderStreamItemIDs))
	mux.HandleFunc("/reader/api/0/stream/items/contents", gr(s.greaderStreamItemContents))
	mux.HandleFunc("/reader/api/0/edit-tag", gr(s.greaderEditTag))
	mux.HandleFunc("/reader/api/0/rename-tag", gr(s.greaderRenameTag))
	mux.HandleFunc("/reader/api/0/disable-tag", gr(s.greaderDisableTag))
	mux.HandleFunc("/reader/api/0/mark-all-as-read", gr(s.greaderMarkAllAsRead))
}

// --- Auth ---

func (s *Server) greaderLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && !(r.Method == http.MethodGet && s.AllowGReaderLoginGET) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := s.clientIP(r)
	if !s.logins.allowed(ip, time.Now()) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	r.ParseForm()
	email := r.FormValue("Email")
	passwd := r.FormValue("Passwd")

	if s.Username != "" || s.Password != "" {
		if !credentialsMatch(email, passwd, s.Username, s.Password) {
			s.logins.failure(ip, time.Now())
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}
	s.logins.success(ip)

	token := generateToken()
	now := time.Now()
	s.tokenMu.Lock()
	// Prune expired tokens so the map cannot grow unboundedly
	for t, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, t)
		}
	}
	s.tokens[token] = now.Add(greaderTokenTTL)
	s.tokenMu.Unlock()

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "SID=%s\nLSID=%s\nAuth=%s\n", token, token, token)
}

func (s *Server) greaderAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "GoogleLogin auth=") {
			w.Header().Set("Google-Bad-Token", "true")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "GoogleLogin auth=")
		s.tokenMu.RLock()
		expiry, ok := s.tokens[token]
		s.tokenMu.RUnlock()
		if !ok || time.Now().After(expiry) {
			w.Header().Set("Google-Bad-Token", "true")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) greaderToken(w http.ResponseWriter, r *http.Request) {
	// CSRF token — clients like Reeder send empty or "x"; we accept anything.
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, generateToken())
}

// --- User info ---

func (s *Server) greaderUserInfo(w http.ResponseWriter, r *http.Request) {
	name := s.Username
	if name == "" {
		name = "user"
	}
	writeJSON(w, map[string]interface{}{
		"userId":              "1",
		"userName":            name,
		"userProfileId":       "1",
		"userEmail":           name,
		"isBloggerUser":       false,
		"signupTimeSec":       0,
		"isMultiLoginEnabled": false,
	})
}

// --- Subscriptions ---

func (s *Server) greaderSubscriptionList(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.db.ListFeeds()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	folders, err := s.db.ListFolders()
	if err != nil {
		httpError(w, err, 500)
		return
	}

	folderMap := make(map[int64]string, len(folders))
	for _, f := range folders {
		folderMap[f.ID] = f.Title
	}

	type category struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type sub struct {
		ID         string     `json:"id"`
		Title      string     `json:"title"`
		URL        string     `json:"url"`
		HTMLURL    string     `json:"htmlUrl"`
		Categories []category `json:"categories"`
	}

	subs := make([]sub, 0, len(feeds))
	for _, f := range feeds {
		cats := []category{}
		if f.FolderID != nil {
			if name, ok := folderMap[*f.FolderID]; ok {
				cats = append(cats, category{
					ID:    "user/-/label/" + name,
					Label: name,
				})
			}
		}
		subs = append(subs, sub{
			ID:         fmt.Sprintf("feed/%d", f.ID),
			Title:      f.Title,
			URL:        f.FeedURL,
			HTMLURL:    f.SiteURL,
			Categories: cats,
		})
	}

	writeJSON(w, map[string]interface{}{"subscriptions": subs})
}

func (s *Server) greaderSubscriptionEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	action := r.FormValue("ac")

	// s= can be repeated; process each
	streamIDs := r.Form["s"]
	titles := r.Form["t"]
	addLabel := r.FormValue("a")
	removeLabel := r.FormValue("r")

	for i, streamID := range streamIDs {
		title := ""
		if i < len(titles) {
			title = titles[i]
		}

		switch action {
		case "subscribe":
			feedURL := strings.TrimPrefix(streamID, "feed/")
			// Resolve folder if label given
			var folderID *int64
			if addLabel != "" {
				label := strings.TrimPrefix(addLabel, "user/-/label/")
				folder, err := s.db.GetOrCreateFolder(label)
				if err != nil {
					httpError(w, err, 500)
					return
				}
				folderID = &folder.ID
			}
			feed, err := s.db.CreateFeed(feedURL, folderID)
			if err != nil {
				httpError(w, err, 500)
				return
			}
			if title != "" {
				if err := s.db.UpdateFeed(feed.ID, &title, folderID); err != nil {
					httpError(w, err, 500)
					return
				}
			}
			s.worker.RefreshFeedAsync(feed.ID)

		case "unsubscribe":
			feed, err := s.streamRefToFeed(streamID)
			if err != nil || feed == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := s.db.DeleteFeed(feed.ID); err != nil {
				httpError(w, err, 500)
				return
			}

		case "edit":
			feed, err := s.streamRefToFeed(streamID)
			if err != nil || feed == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			var titlePtr *string
			if title != "" {
				titlePtr = &title
			}
			if titlePtr != nil {
				if err := s.db.UpdateFeed(feed.ID, titlePtr, nil); err != nil {
					httpError(w, err, 500)
					return
				}
			}
			if addLabel != "" {
				label := strings.TrimPrefix(addLabel, "user/-/label/")
				folder, err := s.db.GetOrCreateFolder(label)
				if err != nil {
					httpError(w, err, 500)
					return
				}
				if err := s.db.UpdateFeedFolder(feed.ID, &folder.ID); err != nil {
					httpError(w, err, 500)
					return
				}
			} else if removeLabel != "" {
				// Move out of folder (UpdateFeed cannot express this; its
				// nil folder_id means "keep current")
				if err := s.db.UpdateFeedFolder(feed.ID, nil); err != nil {
					httpError(w, err, 500)
					return
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "OK")
}

func (s *Server) greaderSubscriptionQuickAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	feedURL := r.FormValue("quickadd")
	if feedURL == "" {
		http.Error(w, "quickadd required", http.StatusBadRequest)
		return
	}

	feed, err := s.db.CreateFeed(feedURL, nil)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	s.worker.RefreshFeedAsync(feed.ID)

	writeJSON(w, map[string]interface{}{
		"numResults": 1,
		"query":      feedURL,
		"streamId":   fmt.Sprintf("feed/%d", feed.ID),
		"streamName": feed.Title,
	})
}

// streamRefToFeed resolves "feed/<numeric_id>" or "feed/<url>" to a Feed.
func (s *Server) streamRefToFeed(streamID string) (*storage.Feed, error) {
	ref := strings.TrimPrefix(streamID, "feed/")
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.db.GetFeed(id)
	}
	return s.db.GetFeedByURL(ref)
}

// --- Tag list ---

func (s *Server) greaderTagList(w http.ResponseWriter, r *http.Request) {
	folders, err := s.db.ListFolders()
	if err != nil {
		httpError(w, err, 500)
		return
	}

	type tag struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}

	tags := []tag{
		{ID: "user/-/state/com.google/starred", Type: ""},
		{ID: "user/-/state/com.google/read", Type: ""},
		{ID: "user/-/state/com.google/reading-list", Type: ""},
	}
	for _, f := range folders {
		tags = append(tags, tag{
			ID:   "user/-/label/" + f.Title,
			Type: "folder",
		})
	}

	writeJSON(w, map[string]interface{}{"tags": tags})
}

// --- Unread count ---

func (s *Server) greaderUnreadCount(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.db.ListFeeds()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	folders, err := s.db.ListFolders()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	stats, err := s.db.GetUnreadCountsByFeed()
	if err != nil {
		httpError(w, err, 500)
		return
	}

	// Build lookup maps
	statMap := make(map[int64]storage.FeedUnreadStat, len(stats))
	for _, st := range stats {
		statMap[st.FeedID] = st
	}
	feedFolderMap := make(map[int64]*int64, len(feeds))
	for _, f := range feeds {
		feedFolderMap[f.ID] = f.FolderID
	}

	type unreadCount struct {
		ID                      string `json:"id"`
		Count                   int64  `json:"count"`
		NewestItemTimestampUsec string `json:"newestItemTimestampUsec"`
	}

	var counts []unreadCount
	var total int64

	// Per-feed counts
	for _, f := range feeds {
		st := statMap[f.ID]
		usec := strconv.FormatInt(st.NewestDate.UnixMicro(), 10)
		counts = append(counts, unreadCount{
			ID:                      fmt.Sprintf("feed/%d", f.ID),
			Count:                   st.Count,
			NewestItemTimestampUsec: usec,
		})
		total += st.Count
	}

	// Per-folder counts (aggregate from feeds)
	folderCount := make(map[int64]storage.FeedUnreadStat)
	for _, f := range feeds {
		if f.FolderID == nil {
			continue
		}
		st := statMap[f.ID]
		agg := folderCount[*f.FolderID]
		agg.Count += st.Count
		if st.NewestDate.After(agg.NewestDate) {
			agg.NewestDate = st.NewestDate
		}
		folderCount[*f.FolderID] = agg
	}
	folderTitleMap := make(map[int64]string, len(folders))
	for _, f := range folders {
		folderTitleMap[f.ID] = f.Title
	}
	for folderID, agg := range folderCount {
		label, ok := folderTitleMap[folderID]
		if !ok {
			continue
		}
		counts = append(counts, unreadCount{
			ID:                      "user/-/label/" + label,
			Count:                   agg.Count,
			NewestItemTimestampUsec: strconv.FormatInt(agg.NewestDate.UnixMicro(), 10),
		})
	}

	// Reading list total
	var newestTotal time.Time
	for _, st := range stats {
		if st.NewestDate.After(newestTotal) {
			newestTotal = st.NewestDate
		}
	}
	counts = append(counts, unreadCount{
		ID:                      "user/-/state/com.google/reading-list",
		Count:                   total,
		NewestItemTimestampUsec: strconv.FormatInt(newestTotal.UnixMicro(), 10),
	})

	writeJSON(w, map[string]interface{}{"max": total, "unreadcounts": counts})
}

// --- Stream ---

func (s *Server) greaderStreamContents(w http.ResponseWriter, r *http.Request) {
	streamID := strings.TrimPrefix(r.URL.Path, "/reader/api/0/stream/contents")
	streamID = strings.TrimPrefix(streamID, "/")
	if streamID == "" {
		streamID = r.URL.Query().Get("s")
	}
	s.serveStreamItems(w, r, streamID)
}

func (s *Server) greaderStreamItemIDs(w http.ResponseWriter, r *http.Request) {
	// NetNewsWire uses Date as the next ot bound. Start it before reading the DB.
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	streamID := r.URL.Query().Get("s")
	filter, err := s.buildStreamFilter(r, streamID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if err := applyItemIDContinuation(&filter, r.URL.Query().Get("c")); err != nil {
		http.Error(w, "invalid continuation", http.StatusBadRequest)
		return
	}

	n := greaderPageSize(r, 1000)
	ids, err := s.db.ListItemIDs(filter, n+1, 0)
	if err != nil {
		httpError(w, err, 500)
		return
	}

	type itemRef struct {
		ID string `json:"id"`
	}
	pageIDs := ids[:min(n, len(ids))]
	refs := make([]itemRef, 0, len(pageIDs))
	for _, id := range pageIDs {
		// FreshRSS returns the database entry ID as a decimal string. Clients
		// such as NetNewsWire convert it to the tagged hexadecimal form when
		// they subsequently request the entry contents.
		refs = append(refs, itemRef{ID: strconv.FormatInt(id, 10)})
	}

	resp := map[string]interface{}{"itemRefs": refs}
	if len(ids) > n {
		resp["continuation"] = strconv.FormatInt(ids[n-1], 10)
	}
	writeJSON(w, resp)
}

func (s *Server) greaderStreamItemContents(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rawIDs := r.Form["i"]

	feedMap, folderTitles := s.loadFeedMaps()

	ids := make([]int64, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		id, err := parseItemID(rawID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	items, batchErr := s.db.GetItemsByIDs(ids)
	entries := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		item := items[id]
		if batchErr != nil {
			// Preserve the reader endpoint's best-effort response on a query error.
			item, _ = s.db.GetItem(id)
		}
		if item == nil {
			continue
		}
		entries = append(entries, itemToGReaderEntry(item, feedMap, folderTitles))
	}

	writeJSON(w, map[string]interface{}{
		// NetNewsWire decodes this endpoint as a Reader API entry wrapper,
		// where both id and updated are required fields.
		"id":      "user/-/state/com.google/reading-list",
		"updated": time.Now().Unix(),
		"items":   entries,
	})
}

func (s *Server) serveStreamItems(w http.ResponseWriter, r *http.Request, streamID string) {
	filter, err := s.buildStreamFilter(r, streamID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if err := applyStreamContinuation(&filter, r.URL.Query().Get("c")); err != nil {
		http.Error(w, "invalid continuation", http.StatusBadRequest)
		return
	}

	n := greaderPageSize(r, 20)
	// Fetch one extra to detect if there are more pages
	items, err := s.db.ListItems(filter, n+1, 0)
	if err != nil {
		httpError(w, err, 500)
		return
	}

	feedMap, folderTitles := s.loadFeedMaps()

	entries := make([]map[string]interface{}, 0, min(n, len(items)))
	for i := range items {
		if i >= n {
			break
		}
		entries = append(entries, itemToGReaderEntry(&items[i], feedMap, folderTitles))
	}

	resp := map[string]interface{}{
		"id":      "user/-/state/com.google/reading-list",
		"updated": time.Now().Unix(),
		"items":   entries,
	}
	if len(items) > n {
		last := items[n-1]
		if filter.OrderByID {
			resp["continuation"] = strconv.FormatInt(last.ID, 10)
		} else {
			resp["continuation"] = encodeItemCursor(last.Date.UTC().Format(time.RFC3339Nano), last.ID)
		}
	}
	writeJSON(w, resp)
}

// loadFeedMaps builds feed and folder title lookup maps in two queries.
func (s *Server) loadFeedMaps() (map[int64]*storage.Feed, map[int64]string) {
	feeds, _ := s.db.ListFeeds()
	feedMap := make(map[int64]*storage.Feed, len(feeds))
	for i := range feeds {
		feedMap[feeds[i].ID] = &feeds[i]
	}

	folders, _ := s.db.ListFolders()
	folderTitles := make(map[int64]string, len(folders))
	for _, f := range folders {
		folderTitles[f.ID] = f.Title
	}

	return feedMap, folderTitles
}

// buildStreamFilter constructs an ItemFilter from a stream ID and request query params.
func greaderPageSize(r *http.Request, fallback int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 && n < math.MaxInt {
		return n
	}
	return fallback
}

func (s *Server) buildStreamFilter(r *http.Request, streamID string) (storage.ItemFilter, error) {
	filter, err := s.streamIDToFilter(streamID)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ItemFilter{NoMatch: true}, nil
	}
	if err != nil {
		return storage.ItemFilter{}, err
	}
	q := r.URL.Query()
	applyStreamState(&filter, q.Get("it"), false)
	applyStreamState(&filter, q.Get("xt"), true)
	filter.StoredAfter = greaderTimeParameter(q.Get("ot"))
	filter.StoredBefore = greaderTimeParameter(q.Get("nt"))
	filter.OldestFirst = q.Get("r") == "o"
	filter.OrderByID = true
	return filter, nil
}

func greaderTimeParameter(value string) *time.Time {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds == 0 {
		return nil
	}
	at := time.Unix(seconds, 0)
	return &at
}

func applyStreamState(filter *storage.ItemFilter, tag string, exclude bool) {
	var status string
	switch tag {
	case "user/-/state/com.google/read":
		status = "read"
		if exclude {
			status = "unread"
		}
	case "user/-/state/com.google/unread", "user/-/state/com.google/kept-unread":
		status = "unread"
		if exclude {
			status = "read"
		}
	case "user/-/state/com.google/starred":
		starred := !exclude
		if filter.Starred != nil && *filter.Starred != starred {
			filter.NoMatch = true
		}
		filter.Starred = &starred
		return
	default:
		return
	}
	if filter.Status != "" && filter.Status != status {
		filter.NoMatch = true
	}
	filter.Status = status
}

// streamIDToFilter maps a GReader stream ID to an ItemFilter.
func (s *Server) streamIDToFilter(streamID string) (storage.ItemFilter, error) {
	filter := storage.ItemFilter{}
	switch {
	case streamID == "" || streamID == "reading-list" || streamID == "user/-/state/com.google/reading-list":
		// The reading list includes all visible items.
	case streamID == "user/-/state/com.google/starred",
		streamID == "user/-/state/com.google/read",
		streamID == "user/-/state/com.google/unread",
		streamID == "user/-/state/com.google/kept-unread":
		applyStreamState(&filter, streamID, false)
	case strings.HasPrefix(streamID, "feed/"):
		ref := strings.TrimPrefix(streamID, "feed/")
		if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
			filter.FeedID = &id
			break
		}
		feed, err := s.db.GetFeedByURL(ref)
		if err != nil {
			return storage.ItemFilter{}, err
		}
		filter.FeedID = &feed.ID
	case strings.HasPrefix(streamID, "user/-/label/"):
		label, ok := greaderFolderName(streamID)
		if !ok {
			return storage.ItemFilter{}, sql.ErrNoRows
		}
		id, err := s.db.GetUniqueFolderIDByTitle(label)
		if err != nil {
			return storage.ItemFilter{}, err
		}
		filter.FolderID = &id
	default:
		return storage.ItemFilter{}, sql.ErrNoRows
	}
	return filter, nil
}

// --- Edit tag (mark read/starred) ---

func (s *Server) greaderEditTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()

	for _, rawID := range r.Form["i"] {
		id, err := parseItemID(rawID)
		if err != nil {
			continue
		}
		for _, tag := range r.Form["a"] {
			switch tag {
			case "user/-/state/com.google/read":
				if err := s.db.UpdateItemStatus(id, "read"); err != nil {
					httpError(w, err, 500)
					return
				}
			case "user/-/state/com.google/starred":
				if err := s.db.SetStarred(id, true); err != nil {
					httpError(w, err, 500)
					return
				}
			case "user/-/state/com.google/kept-unread":
				if err := s.db.UpdateItemStatus(id, "unread"); err != nil {
					httpError(w, err, 500)
					return
				}
			}
		}
		for _, tag := range r.Form["r"] {
			switch tag {
			case "user/-/state/com.google/read":
				if err := s.db.UpdateItemStatus(id, "unread"); err != nil {
					httpError(w, err, 500)
					return
				}
			case "user/-/state/com.google/starred":
				if err := s.db.SetStarred(id, false); err != nil {
					httpError(w, err, 500)
					return
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "OK")
}

// --- Mark all as read ---

func (s *Server) greaderMarkAllAsRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	streams := r.Form["s"]
	if len(streams) != 1 || strings.TrimSpace(streams[0]) == "" {
		http.Error(w, "exactly one stream required", http.StatusBadRequest)
		return
	}
	streamID := strings.TrimSpace(streams[0])
	if streamID == "reading-list" {
		http.Error(w, "use the full reading-list stream ID", http.StatusBadRequest)
		return
	}
	filter, err := s.streamIDToFilter(streamID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unknown stream", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	if len(r.Form["ts"]) > 1 {
		http.Error(w, "multiple timestamps", http.StatusBadRequest)
		return
	}
	// For existing callers, a nonzero ts is a publication cutoff in nanoseconds.
	if value := strings.TrimSpace(r.FormValue("ts")); value != "" {
		tsNano, err := strconv.ParseInt(value, 10, 64)
		if err != nil || tsNano < 0 {
			http.Error(w, "invalid timestamp", http.StatusBadRequest)
			return
		}
		if tsNano > 0 {
			t := time.Unix(0, tsNano)
			filter.Before = &t
		}
	}

	if err := s.db.MarkAllRead(filter); err != nil {
		httpError(w, err, 500)
		return
	}

	writeGReaderOK(w)
}

// --- Helpers ---

// parseItemID strips the "tag:google.com,2005:reader/item/" prefix and parses the hex ID.
func parseItemID(raw string) (int64, error) {
	const tagPrefix = "tag:google.com,2005:reader/item/"
	if strings.HasPrefix(raw, tagPrefix) {
		return strconv.ParseInt(strings.TrimPrefix(raw, tagPrefix), 16, 64)
	}
	// FreshRSS accepts decimal IDs directly in stream/items/contents, while
	// Google Reader clients commonly send the tagged hexadecimal representation.
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return id, nil
	}
	return strconv.ParseInt(raw, 16, 64)
}

func applyItemIDContinuation(filter *storage.ItemFilter, value string) error {
	if value == "" || value == "0" {
		return nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("invalid continuation")
	}
	filter.Cursor = &storage.ItemCursor{ID: id}
	return nil
}

func applyStreamContinuation(filter *storage.ItemFilter, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return applyItemIDContinuation(filter, value)
	}
	// Old versions issued a date-and-ID token for stream/contents.
	cursor, err := decodeItemCursor(value)
	if err != nil || cursor == nil {
		return err
	}
	date, err := time.Parse(time.RFC3339Nano, cursor.Date)
	if err != nil {
		return err
	}
	cursor.Date = date.UTC().Format(time.RFC3339Nano)
	filter.OrderByID = false
	filter.Cursor = cursor
	return nil
}

// itemToGReaderEntry converts a storage.Item to a GReader API entry map.
func itemToGReaderEntry(item *storage.Item, feedMap map[int64]*storage.Feed, folderTitles map[int64]string) map[string]interface{} {
	categories := []string{"user/-/state/com.google/reading-list"}
	if item.Status == "read" {
		categories = append(categories, "user/-/state/com.google/read")
	}
	if item.Starred {
		categories = append(categories, "user/-/state/com.google/starred")
	}

	feedStreamID := fmt.Sprintf("feed/%d", item.FeedID)
	feedTitle := ""
	if feed, ok := feedMap[item.FeedID]; ok {
		feedTitle = feed.Title
		if feed.FolderID != nil {
			if label, ok := folderTitles[*feed.FolderID]; ok {
				categories = append(categories, "user/-/label/"+label)
			}
		}
	}

	return map[string]interface{}{
		"id":         fmt.Sprintf("tag:google.com,2005:reader/item/%016x", item.ID),
		"title":      item.Title,
		"author":     item.Author,
		"categories": categories,
		"published":  item.Date.Unix(),
		"updated":    item.Date.Unix(),
		"canonical":  []map[string]string{{"href": item.Link}},
		"alternate":  []map[string]string{{"href": item.Link, "type": "text/html"}},
		"summary":    map[string]string{"direction": "ltr", "content": item.Content},
		"content":    map[string]string{"direction": "ltr", "content": item.Content},
		"origin": map[string]interface{}{
			"streamId": feedStreamID,
			"title":    feedTitle,
		},
	}
}
