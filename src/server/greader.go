package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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
	mux.HandleFunc("/reader/api/0/unread-count", gr(s.greaderUnreadCount))
	mux.HandleFunc("/reader/api/0/tag/list", gr(s.greaderTagList))
	mux.HandleFunc("/reader/api/0/stream/contents/", gr(s.greaderStreamContents))
	mux.HandleFunc("/reader/api/0/stream/items/ids", gr(s.greaderStreamItemIDs))
	mux.HandleFunc("/reader/api/0/stream/items/contents", gr(s.greaderStreamItemContents))
	mux.HandleFunc("/reader/api/0/edit-tag", gr(s.greaderEditTag))
	mux.HandleFunc("/reader/api/0/mark-all-as-read", gr(s.greaderMarkAllAsRead))
}

// --- Auth ---

func (s *Server) greaderLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && !(r.Method == http.MethodGet && s.AllowGReaderLoginGET) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := clientIP(r)
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
				if folder, err := s.db.GetOrCreateFolder(label); err == nil {
					folderID = &folder.ID
				}
			}
			feed, err := s.db.CreateFeed(feedURL, folderID)
			if err != nil {
				httpError(w, err, 500)
				return
			}
			if title != "" {
				s.db.UpdateFeed(feed.ID, &title, folderID)
			}
			go s.worker.RefreshFeed(feed.ID)

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
				s.db.UpdateFeed(feed.ID, titlePtr, nil)
			}
			if addLabel != "" {
				label := strings.TrimPrefix(addLabel, "user/-/label/")
				if folder, err := s.db.GetOrCreateFolder(label); err == nil {
					s.db.UpdateFeedFolder(feed.ID, &folder.ID)
				}
			} else if removeLabel != "" {
				// Move out of folder (UpdateFeed cannot express this; its
				// nil folder_id means "keep current")
				s.db.UpdateFeedFolder(feed.ID, nil)
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
	go s.worker.RefreshFeed(feed.ID)

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
	streamID := strings.TrimPrefix(r.URL.Path, "/reader/api/0/stream/contents/")
	if streamID == "" {
		streamID = r.URL.Query().Get("s")
	}
	s.serveStreamItems(w, r, streamID)
}

func (s *Server) greaderStreamItemIDs(w http.ResponseWriter, r *http.Request) {
	streamID := r.URL.Query().Get("s")
	filter := s.buildStreamFilter(r, streamID)
	if err := s.applyItemIDContinuation(&filter, r.URL.Query().Get("c")); err != nil {
		http.Error(w, "invalid continuation", http.StatusBadRequest)
		return
	}

	n := 1000
	if ns := r.URL.Query().Get("n"); ns != "" {
		if parsed, err := strconv.Atoi(ns); err == nil && parsed > 0 {
			n = parsed
		}
	}
	items, err := s.db.ListItems(filter, n+1, 0)
	if err != nil {
		httpError(w, err, 500)
		return
	}

	type itemRef struct {
		ID string `json:"id"`
	}
	refs := make([]itemRef, 0, len(items))
	for i := range items {
		if i >= n {
			break
		}
		// FreshRSS returns the database entry ID as a decimal string. Clients
		// such as NetNewsWire convert it to the tagged hexadecimal form when
		// they subsequently request the entry contents.
		refs = append(refs, itemRef{ID: strconv.FormatInt(items[i].ID, 10)})
	}

	resp := map[string]interface{}{"itemRefs": refs}
	if len(items) > n {
		resp["continuation"] = strconv.FormatInt(items[n-1].ID, 10)
	}
	writeJSON(w, resp)
}

func (s *Server) greaderStreamItemContents(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rawIDs := r.Form["i"]

	feedMap, folderTitles := s.loadFeedMaps()

	entries := make([]map[string]interface{}, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		id, err := parseItemID(rawID)
		if err != nil {
			continue
		}
		item, err := s.db.GetItem(id)
		if err != nil || item == nil {
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
	filter := s.buildStreamFilter(r, streamID)
	if err := applyContinuation(&filter, r.URL.Query().Get("c")); err != nil {
		http.Error(w, "invalid continuation", http.StatusBadRequest)
		return
	}

	n := 20
	if ns := r.URL.Query().Get("n"); ns != "" {
		if parsed, err := strconv.Atoi(ns); err == nil && parsed > 0 {
			n = parsed
		}
	}
	// Fetch one extra to detect if there are more pages
	items, err := s.db.ListItems(filter, n+1, 0)
	if err != nil {
		httpError(w, err, 500)
		return
	}

	feedMap, folderTitles := s.loadFeedMaps()

	entries := make([]map[string]interface{}, 0, n)
	for i := range items {
		if i >= n {
			break
		}
		entries = append(entries, itemToGReaderEntry(&items[i], feedMap, folderTitles))
	}

	resp := map[string]interface{}{
		"id":    streamID,
		"items": entries,
	}
	if len(items) > n {
		resp["continuation"] = encodeContinuation(items[n-1])
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
func (s *Server) buildStreamFilter(r *http.Request, streamID string) storage.ItemFilter {
	filter := s.streamIDToFilter(streamID)

	// xt= exclude tag (most common: xt=user/-/state/com.google/read → show unread only)
	if xt := r.URL.Query().Get("xt"); xt != "" {
		if xt == "user/-/state/com.google/read" && filter.Status == "" {
			filter.Status = "unread"
		}
	}

	// it= include tag (override status/starred filter)
	if it := r.URL.Query().Get("it"); it != "" {
		f := s.streamIDToFilter(it)
		if f.Status != "" {
			filter.Status = f.Status
		}
		if f.Starred != nil {
			filter.Starred = f.Starred
		}
	}

	// ot= items published after this Unix timestamp
	if ot := r.URL.Query().Get("ot"); ot != "" {
		if ts, err := strconv.ParseInt(ot, 10, 64); err == nil {
			t := time.Unix(ts, 0)
			filter.After = &t
		}
	}

	// r= sort order: "o" = oldest first
	if r.URL.Query().Get("r") == "o" {
		filter.OldestFirst = true
	}

	return filter
}

// streamIDToFilter maps a GReader stream ID to an ItemFilter.
func (s *Server) streamIDToFilter(streamID string) storage.ItemFilter {
	filter := storage.ItemFilter{}
	trueVal := true
	switch {
	case streamID == "" || streamID == "user/-/state/com.google/reading-list":
		// all items — no filter
	case streamID == "user/-/state/com.google/starred":
		filter.Starred = &trueVal
	case streamID == "user/-/state/com.google/read":
		filter.Status = "read"
	case streamID == "user/-/state/com.google/unread",
		streamID == "user/-/state/com.google/kept-unread":
		filter.Status = "unread"
	case strings.HasPrefix(streamID, "feed/"):
		idStr := strings.TrimPrefix(streamID, "feed/")
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			filter.FeedID = &id
		}
	case strings.HasPrefix(streamID, "user/-/label/"):
		label := strings.TrimPrefix(streamID, "user/-/label/")
		if folder, err := s.db.GetFolderByTitle(label); err == nil {
			filter.FolderID = &folder.ID
		}
	}
	return filter
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
				s.db.UpdateItemStatus(id, "read")
			case "user/-/state/com.google/starred":
				s.db.SetStarred(id, true)
			case "user/-/state/com.google/kept-unread":
				s.db.UpdateItemStatus(id, "unread")
			}
		}
		for _, tag := range r.Form["r"] {
			switch tag {
			case "user/-/state/com.google/read":
				s.db.UpdateItemStatus(id, "unread")
			case "user/-/state/com.google/starred":
				s.db.SetStarred(id, false)
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
	r.ParseForm()

	filter := s.streamIDToFilter(r.FormValue("s"))

	// ts= nanoseconds — mark only items published before this timestamp
	if tsStr := r.FormValue("ts"); tsStr != "" {
		if tsNano, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			t := time.Unix(0, tsNano)
			filter.Before = &t
		}
	}

	s.db.MarkAllRead(filter)

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "OK")
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

// applyItemIDContinuation resolves FreshRSS's decimal entry-ID continuation
// into the date-and-ID cursor needed by the storage query.
func (s *Server) applyItemIDContinuation(filter *storage.ItemFilter, value string) error {
	if value == "" {
		return nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("invalid continuation")
	}
	item, err := s.db.GetItem(id)
	if err != nil || item == nil {
		return fmt.Errorf("invalid continuation")
	}
	filter.Cursor = &storage.ItemCursor{Date: item.Date, ID: item.ID}
	return nil
}

func encodeContinuation(item storage.Item) string {
	payload := item.Date.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(item.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func applyContinuation(filter *storage.ItemFilter, value string) error {
	if value == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	parts := strings.Split(string(payload), "|")
	if len(parts) != 2 {
		return fmt.Errorf("invalid continuation")
	}
	date, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("invalid continuation")
	}
	filter.Cursor = &storage.ItemCursor{Date: date, ID: id}
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
