package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nkanaev/yarr2/src/storage"
)

// --- Test helpers ---

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	f, err := os.CreateTemp("", "yarr2-server-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := storage.Open(f.Name())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := New(db, "localhost:0", "", "")
	ts := httptest.NewServer(srv.buildMux())
	t.Cleanup(ts.Close)
	return srv, ts
}

func (s *Server) buildMux() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.registerGReaderRoutes(mux)
	return mux
}

// login returns an auth token for use in subsequent requests.
func login(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, err := http.PostForm(ts.URL+"/accounts/ClientLogin", url.Values{
		"Email":  {"user"},
		"Passwd": {"pass"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "Auth=") {
			return strings.TrimPrefix(strings.TrimSpace(line), "Auth=")
		}
	}
	t.Fatalf("no Auth= in ClientLogin response: %s", string(body))
	return ""
}

func grGet(t *testing.T, ts *httptest.Server, token, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	req.Header.Set("Authorization", "GoogleLogin auth="+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func grPost(t *testing.T, ts *httptest.Server, token, path string, vals url.Values) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(vals.Encode()))
	req.Header.Set("Authorization", "GoogleLogin auth="+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
}

func seedFeed(t *testing.T, db *storage.Storage, feedURL, folderName string) *storage.Feed {
	t.Helper()
	var folderID *int64
	if folderName != "" {
		folder, _ := db.GetOrCreateFolder(folderName)
		folderID = &folder.ID
	}
	feed, err := db.CreateFeed(feedURL, folderID)
	if err != nil {
		t.Fatal(err)
	}
	db.UpdateFeedMeta(feed.ID, "Test Feed", "https://site.example.com")
	return feed
}

func seedItems(t *testing.T, db *storage.Storage, feedID int64, n int) []storage.Item {
	t.Helper()
	items := make([]storage.Item, n)
	for i := range items {
		items[i] = storage.Item{
			FeedID:  feedID,
			GUID:    fmt.Sprintf("guid-%d", i),
			Title:   fmt.Sprintf("Item %d", i),
			Link:    fmt.Sprintf("https://example.com/%d", i),
			Date:    time.Now().Add(time.Duration(i) * time.Minute),
			Content: fmt.Sprintf("<p>Content %d</p>", i),
			Author:  "Author",
		}
	}
	if err := db.CreateItems(items); err != nil {
		t.Fatal(err)
	}
	// Re-read to get IDs
	got, _ := db.ListItems(storage.ItemFilter{FeedID: &feedID}, n+10, 0)
	return got
}

// --- Tests ---

func TestGreaderLogin(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.PostForm(ts.URL+"/accounts/ClientLogin", url.Values{
		"Email":  {"user"},
		"Passwd": {"pass"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Auth=") {
		t.Errorf("response should contain Auth= token, got: %s", string(body))
	}
}

func TestGreaderLoginGET(t *testing.T) {
	_, ts := newTestServer(t)

	// Some clients send GET
	resp, err := http.Get(ts.URL + "/accounts/ClientLogin?Email=user&Passwd=pass")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET ClientLogin status = %d, want 200", resp.StatusCode)
	}
}

func TestGreaderUnauthorized(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/reader/api/0/user-info")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("Google-Bad-Token") != "true" {
		t.Error("should set Google-Bad-Token: true header")
	}
}

func TestGreaderUserInfo(t *testing.T) {
	_, ts := newTestServer(t)
	token := login(t, ts)

	resp := grGet(t, ts, token, "/reader/api/0/user-info")
	var info map[string]interface{}
	decodeJSON(t, resp, &info)

	if info["userId"] == nil {
		t.Error("userId missing")
	}
}

func TestGreaderSubscriptionList(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	seedFeed(t, srv.db, "https://example.com/feed.xml", "Tech")
	seedFeed(t, srv.db, "https://other.com/feed.xml", "")

	resp := grGet(t, ts, token, "/reader/api/0/subscription/list")
	var result struct {
		Subscriptions []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Categories []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"categories"`
		} `json:"subscriptions"`
	}
	decodeJSON(t, resp, &result)

	if len(result.Subscriptions) != 2 {
		t.Fatalf("subscriptions count = %d, want 2", len(result.Subscriptions))
	}

	// IDs should be numeric (feed/<id>), not URLs
	for _, sub := range result.Subscriptions {
		if !strings.HasPrefix(sub.ID, "feed/") {
			t.Errorf("subscription ID %q should start with feed/", sub.ID)
		}
		parts := strings.SplitN(sub.ID, "/", 2)
		if _, err := fmt.Sscanf(parts[1], "%d", new(int64)); err != nil {
			t.Errorf("subscription ID %q should have numeric part", sub.ID)
		}
	}

	// Feed in folder should have category
	found := false
	for _, sub := range result.Subscriptions {
		if len(sub.Categories) > 0 {
			found = true
			if sub.Categories[0].Label != "Tech" {
				t.Errorf("category label = %q, want 'Tech'", sub.Categories[0].Label)
			}
		}
	}
	if !found {
		t.Error("no subscription had categories")
	}
}

func TestGreaderSubscriptionEditSubscribe(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	resp := grPost(t, ts, token, "/reader/api/0/subscription/edit", url.Values{
		"ac": {"subscribe"},
		"s":  {"feed/https://new.example.com/feed.xml"},
		"t":  {"New Feed"},
		"a":  {"user/-/label/News"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	feeds, _ := srv.db.ListFeeds()
	if len(feeds) != 1 {
		t.Fatalf("feeds count = %d, want 1", len(feeds))
	}
	// Folder should be created
	folders, _ := srv.db.ListFolders()
	if len(folders) != 1 || folders[0].Title != "News" {
		t.Error("folder 'News' should be created")
	}
}

func TestGreaderSubscriptionEditUnsubscribe(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")

	resp := grPost(t, ts, token, "/reader/api/0/subscription/edit", url.Values{
		"ac": {"unsubscribe"},
		"s":  {fmt.Sprintf("feed/%d", feed.ID)},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	feeds, _ := srv.db.ListFeeds()
	if len(feeds) != 0 {
		t.Error("feed should be deleted")
	}
}

func TestGreaderTagList(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	srv.db.GetOrCreateFolder("Sports")
	srv.db.GetOrCreateFolder("Tech")

	resp := grGet(t, ts, token, "/reader/api/0/tag/list")
	var result struct {
		Tags []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"tags"`
	}
	decodeJSON(t, resp, &result)

	hasStarred := false
	folderCount := 0
	for _, tag := range result.Tags {
		if tag.ID == "user/-/state/com.google/starred" {
			hasStarred = true
		}
		if tag.Type == "folder" {
			folderCount++
		}
	}
	if !hasStarred {
		t.Error("starred tag missing")
	}
	if folderCount != 2 {
		t.Errorf("folder tags = %d, want 2", folderCount)
	}
}

func TestGreaderUnreadCount(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "Tech")
	items := seedItems(t, srv.db, feed.ID, 3)

	// Mark one as read
	srv.db.UpdateItemStatus(items[0].ID, "read")

	resp := grGet(t, ts, token, "/reader/api/0/unread-count")
	var result struct {
		Max          int64 `json:"max"`
		UnreadCounts []struct {
			ID    string `json:"id"`
			Count int64  `json:"count"`
		} `json:"unreadcounts"`
	}
	decodeJSON(t, resp, &result)

	if result.Max != 2 {
		t.Errorf("max = %d, want 2", result.Max)
	}

	hasReadingList := false
	for _, uc := range result.UnreadCounts {
		if uc.ID == "user/-/state/com.google/reading-list" {
			hasReadingList = true
			if uc.Count != 2 {
				t.Errorf("reading-list count = %d, want 2", uc.Count)
			}
		}
	}
	if !hasReadingList {
		t.Error("reading-list entry missing from unread-count")
	}
}

func TestGreaderStreamContents(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	seedItems(t, srv.db, feed.ID, 5)

	resp := grGet(t, ts, token, "/reader/api/0/stream/contents/user/-/state/com.google/reading-list?n=3")
	var result struct {
		Items        []map[string]interface{} `json:"items"`
		Continuation string                   `json:"continuation"`
	}
	decodeJSON(t, resp, &result)

	if len(result.Items) != 3 {
		t.Errorf("items count = %d, want 3", len(result.Items))
	}
	if result.Continuation == "" {
		t.Error("continuation should be set when more items exist")
	}

	// Item should have required fields
	item := result.Items[0]
	for _, field := range []string{"id", "title", "published", "categories", "origin"} {
		if item[field] == nil {
			t.Errorf("item missing field: %s", field)
		}
	}

	// ID format: tag:google.com,...
	idStr, _ := item["id"].(string)
	if !strings.HasPrefix(idStr, "tag:google.com,2005:reader/item/") {
		t.Errorf("item id = %q, wrong format", idStr)
	}
}

func TestGreaderStreamContentsUnreadFilter(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	items := seedItems(t, srv.db, feed.ID, 4)
	srv.db.UpdateItemStatus(items[0].ID, "read")
	srv.db.UpdateItemStatus(items[1].ID, "read")

	// xt=read → show only unread
	resp := grGet(t, ts, token, "/reader/api/0/stream/contents/user/-/state/com.google/reading-list?xt=user/-/state/com.google/read")
	var result struct {
		Items []map[string]interface{} `json:"items"`
	}
	decodeJSON(t, resp, &result)

	if len(result.Items) != 2 {
		t.Errorf("unread items = %d, want 2", len(result.Items))
	}
}

func TestGreaderStreamContentsFeedFilter(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed1 := seedFeed(t, srv.db, "https://a.example.com/feed.xml", "")
	feed2 := seedFeed(t, srv.db, "https://b.example.com/feed.xml", "")
	seedItems(t, srv.db, feed1.ID, 3)
	seedItems(t, srv.db, feed2.ID, 2)

	path := fmt.Sprintf("/reader/api/0/stream/contents/feed/%d", feed1.ID)
	resp := grGet(t, ts, token, path)
	var result struct {
		Items []map[string]interface{} `json:"items"`
	}
	decodeJSON(t, resp, &result)

	if len(result.Items) != 3 {
		t.Errorf("feed1 items = %d, want 3", len(result.Items))
	}
}

func TestGreaderStreamContentsPagination(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	seedItems(t, srv.db, feed.ID, 10)

	// First page
	resp := grGet(t, ts, token, "/reader/api/0/stream/contents/user/-/state/com.google/reading-list?n=4")
	var page1 struct {
		Items        []map[string]interface{} `json:"items"`
		Continuation string                   `json:"continuation"`
	}
	decodeJSON(t, resp, &page1)

	if len(page1.Items) != 4 {
		t.Fatalf("page1 items = %d, want 4", len(page1.Items))
	}
	if page1.Continuation == "" {
		t.Fatal("page1 continuation should not be empty")
	}

	// Second page
	path := "/reader/api/0/stream/contents/user/-/state/com.google/reading-list?n=4&c=" + page1.Continuation
	resp2 := grGet(t, ts, token, path)
	var page2 struct {
		Items []map[string]interface{} `json:"items"`
	}
	decodeJSON(t, resp2, &page2)

	if len(page2.Items) != 4 {
		t.Fatalf("page2 items = %d, want 4", len(page2.Items))
	}

	// No overlap
	ids1 := map[string]bool{}
	for _, item := range page1.Items {
		ids1[item["id"].(string)] = true
	}
	for _, item := range page2.Items {
		if ids1[item["id"].(string)] {
			t.Error("duplicate item across pages")
		}
	}
}

func TestGreaderStreamItemIDs(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	seedItems(t, srv.db, feed.ID, 3)

	resp := grGet(t, ts, token, "/reader/api/0/stream/items/ids?s=user/-/state/com.google/reading-list")
	var result struct {
		ItemRefs []struct {
			ID string `json:"id"`
		} `json:"itemRefs"`
	}
	decodeJSON(t, resp, &result)

	if len(result.ItemRefs) != 3 {
		t.Fatalf("itemRefs count = %d, want 3", len(result.ItemRefs))
	}
	// IDs should be hex strings without prefix
	for _, ref := range result.ItemRefs {
		if strings.HasPrefix(ref.ID, "tag:") {
			t.Errorf("itemRef ID %q should be bare hex, not tag format", ref.ID)
		}
		if len(ref.ID) != 16 {
			t.Errorf("itemRef ID %q should be 16 hex chars", ref.ID)
		}
	}
}

func TestGreaderEditTagMarkRead(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	items := seedItems(t, srv.db, feed.ID, 2)

	itemID := fmt.Sprintf("tag:google.com,2005:reader/item/%016x", items[0].ID)
	resp := grPost(t, ts, token, "/reader/api/0/edit-tag", url.Values{
		"i": {itemID},
		"a": {"user/-/state/com.google/read"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	updated, _ := srv.db.GetItem(items[0].ID)
	if updated.Status != "read" {
		t.Errorf("status = %q, want 'read'", updated.Status)
	}
}

func TestGreaderEditTagMarkUnread(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	items := seedItems(t, srv.db, feed.ID, 1)
	srv.db.UpdateItemStatus(items[0].ID, "read")

	itemID := fmt.Sprintf("tag:google.com,2005:reader/item/%016x", items[0].ID)
	grPost(t, ts, token, "/reader/api/0/edit-tag", url.Values{
		"i": {itemID},
		"r": {"user/-/state/com.google/read"},
	})

	updated, _ := srv.db.GetItem(items[0].ID)
	if updated.Status != "unread" {
		t.Errorf("status = %q, want 'unread'", updated.Status)
	}
}

func TestGreaderEditTagStar(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	items := seedItems(t, srv.db, feed.ID, 1)

	itemID := fmt.Sprintf("tag:google.com,2005:reader/item/%016x", items[0].ID)
	grPost(t, ts, token, "/reader/api/0/edit-tag", url.Values{
		"i": {itemID},
		"a": {"user/-/state/com.google/starred"},
	})

	updated, _ := srv.db.GetItem(items[0].ID)
	if !updated.Starred {
		t.Errorf("Starred = false, want true after starring")
	}
}

func TestGreaderMarkAllAsRead(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	seedItems(t, srv.db, feed.ID, 5)

	resp := grPost(t, ts, token, "/reader/api/0/mark-all-as-read", url.Values{
		"s": {fmt.Sprintf("feed/%d", feed.ID)},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	count, _ := srv.db.CountItems(storage.ItemFilter{Status: "unread"})
	if count != 0 {
		t.Errorf("unread count = %d after mark-all-as-read, want 0", count)
	}
}

func TestGreaderMarkAllAsReadWithTimestamp(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")

	// Insert items with explicit dates
	cutoff := time.Now()
	db := srv.db
	db.CreateItems([]storage.Item{
		{FeedID: feed.ID, GUID: "old", Title: "Old", Link: "https://x.com/1", Date: cutoff.Add(-time.Hour)},
		{FeedID: feed.ID, GUID: "new", Title: "New", Link: "https://x.com/2", Date: cutoff.Add(time.Hour)},
	})

	// Mark only items before cutoff (nanoseconds)
	tsNano := fmt.Sprintf("%d", cutoff.UnixNano())
	grPost(t, ts, token, "/reader/api/0/mark-all-as-read", url.Values{
		"s":  {"user/-/state/com.google/reading-list"},
		"ts": {tsNano},
	})

	unread, _ := db.ListItems(storage.ItemFilter{Status: "unread"}, 10, 0)
	if len(unread) != 1 {
		t.Errorf("unread after ts-based mark = %d, want 1 (the new item)", len(unread))
	}
	if len(unread) == 1 && unread[0].Title != "New" {
		t.Errorf("remaining unread = %q, want 'New'", unread[0].Title)
	}
}

func TestGreaderQuickAdd(t *testing.T) {
	srv, ts := newTestServer(t)
	token := login(t, ts)

	resp := grPost(t, ts, token, "/reader/api/0/subscription/quickadd", url.Values{
		"quickadd": {"https://quick.example.com/feed.xml"},
	})
	var result map[string]interface{}
	decodeJSON(t, resp, &result)

	if result["numResults"].(float64) != 1 {
		t.Error("numResults should be 1")
	}
	if !strings.HasPrefix(result["streamId"].(string), "feed/") {
		t.Errorf("streamId = %q, should start with feed/", result["streamId"])
	}

	feeds, _ := srv.db.ListFeeds()
	if len(feeds) != 1 {
		t.Errorf("feed count = %d, want 1", len(feeds))
	}
}

