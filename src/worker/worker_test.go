package worker

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

func TestMain(m *testing.M) {
	// Worker integration tests use httptest loopback servers. Production clients
	// remain guarded; the guard itself is covered in safehttp package tests.
	httpClient = &http.Client{Timeout: 30 * time.Second}
	crawlerClient = &http.Client{Timeout: 15 * time.Second}
	os.Exit(m.Run())
}

// --- helpers ---

func newTestDB(t *testing.T) *storage.Storage {
	t.Helper()
	return openTestDB(t, filepath.Join(t.TempDir(), "hayari.db"))
}

func openTestDB(t *testing.T, path string) *storage.Storage {
	t.Helper()
	s, err := storage.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	// Most worker tests use historical fixture dates. Disable the age limit in
	// those fixtures; age-limit behavior has focused tests below.
	if err := s.SetSetting("item_max_age_days", "0"); err != nil {
		t.Fatal(err)
	}
	return s
}

const rssSample = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>%s</link>
    <item>
      <guid>item-1</guid>
      <title>Hello World</title>
      <link>%s/post/1</link>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
      <description>First post</description>
    </item>
    <item>
      <guid>item-2</guid>
      <title>Second Post</title>
      <link>%s/post/2</link>
      <pubDate>Tue, 02 Jan 2024 00:00:00 +0000</pubDate>
      <description>Second post</description>
    </item>
  </channel>
</rss>`

// newFeedServer returns a test HTTP server serving an RSS feed.
func newFeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		body := fmt.Sprintf(rssSample, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- RefreshFeed ---

func TestRefreshFeedInsertsItems(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	w := New(db)
	w.RefreshFeed(feed.ID)

	items, err := db.ListItems(storage.ItemFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("items count = %d, want 2", len(items))
	}
}

func TestRefreshFeedSkipsTitleFilterKeywords(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	if err := db.UpdateFeedTitleFilterKeywords(feed.ID, "hello,Second"); err != nil {
		t.Fatal(err)
	}
	w := New(db)
	w.RefreshFeed(feed.ID)

	count, err := db.CountItems(storage.ItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("item count = %d, want 0 after OR title filters", count)
	}
}

func TestRefreshFeedGenericHideFilterHidesItems(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)
	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	if _, err := db.CreateFilter(storage.Filter{
		FeedID: &feed.ID,
		Rule:   "Hello",
		Action: "hide",
		Field:  "title",
	}); err != nil {
		t.Fatal(err)
	}

	New(db).RefreshFeed(feed.ID)
	items, err := db.ListItems(storage.ItemFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Second Post" {
		t.Fatalf("visible items = %#v, want only non-hidden item", items)
	}
}

func TestRefreshFeedUpdatesMeta(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	if feed.Title != "" {
		t.Fatal("title should be empty initially")
	}

	w := New(db)
	w.RefreshFeed(feed.ID)

	updated, _ := db.GetFeed(feed.ID)
	if updated.Title != "Test Feed" {
		t.Errorf("Title = %q, want %q", updated.Title, "Test Feed")
	}
}

func TestRefreshFeedRecordsError(t *testing.T) {
	db := newTestDB(t)

	// Point to an invalid URL
	feed, _ := db.CreateFeed("http://127.0.0.1:1/dead-feed.xml", nil)

	w := New(db)
	w.RefreshFeed(feed.ID)

	updated, _ := db.GetFeed(feed.ID)
	if updated.LastError == nil || *updated.LastError == "" {
		t.Error("LastError should be set after a failed fetch")
	}
}

func TestFetchRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", maxFeedSize+1))
		fmt.Fprint(w, strings.Repeat("x", maxFeedSize+1))
	}))
	defer srv.Close()
	if _, err := Fetch(srv.URL, nil, nil); err == nil {
		t.Fatal("oversized feed response was accepted")
	}
}

func TestUserAgentUsesReleaseVersion(t *testing.T) {
	SetVersion("v1.2.3")
	t.Cleanup(func() { SetVersion("dev") })

	if got, want := UserAgent(), "Hayari/1.2.3 (+https://forge.harakara.site/littleisland/hayari)"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

func TestRefreshFeedClearsErrorOnSuccess(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	// Pre-set an error
	db.UpdateFeedError(feed.ID, "previous error")

	w := New(db)
	w.RefreshFeed(feed.ID)

	updated, _ := db.GetFeed(feed.ID)
	if updated.LastError != nil {
		t.Errorf("LastError should be cleared after success, got %q", *updated.LastError)
	}
}

func TestRefreshFeedSkips304(t *testing.T) {
	db := newTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"etag123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag123"`)
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, rssSample, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
	}))
	defer srv.Close()

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	w := New(db)

	// First refresh — fetches and stores ETag
	w.RefreshFeed(feed.ID)
	count1, _ := db.CountItems(storage.ItemFilter{})

	// Second refresh — server returns 304, no new items
	w.RefreshFeed(feed.ID)
	count2, _ := db.CountItems(storage.ItemFilter{})

	if count1 != count2 {
		t.Errorf("item count changed on 304: %d → %d", count1, count2)
	}
}

func TestRefreshFeedRetriesWhenArticleInsertFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hayari.db")
	db := openTestDB(t, path)
	admin, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE TRIGGER reject_second_item BEFORE INSERT ON items
		WHEN NEW.guid = 'item-2' BEGIN SELECT RAISE(ABORT, 'failed test insert'); END`); err != nil {
		t.Fatal(err)
	}

	const etag = `"revision-one"`
	const modified = "Mon, 01 Jan 2024 00:00:00 GMT"
	var conditionalRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag || r.Header.Get("If-Modified-Since") == modified {
			conditionalRequests.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", modified)
		fmt.Fprintf(w, rssSample, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
	}))
	defer srv.Close()
	feed := createFeedWithIcon(t, db, srv.URL+"/feed")
	w := New(db)
	w.RefreshFeed(feed.ID)
	if got := w.ArticleRevision(); got != 0 {
		t.Fatalf("article revision after failed insert = %d, want 0", got)
	}
	updated, err := db.GetFeed(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastRefreshed != nil || updated.ETag != nil || updated.LastModified != nil || updated.LastError == nil {
		t.Fatalf("failed refresh should only record the error: %+v", updated)
	}
	for _, filter := range []storage.ItemFilter{{}, {Search: "Hello"}} {
		if count, err := db.CountItems(filter); err != nil || count != 0 {
			t.Fatalf("rolled back item count for %+v = %d, %v", filter, count, err)
		}
	}
	var searchRows int
	if err := admin.QueryRow(`SELECT COUNT(*) FROM search`).Scan(&searchRows); err != nil || searchRows != 0 {
		t.Fatalf("rolled back search rows = %d, %v", searchRows, err)
	}
	if _, err := admin.Exec(`DROP TRIGGER reject_second_item`); err != nil {
		t.Fatal(err)
	}
	w.RefreshFeed(feed.ID)
	if conditionalRequests.Load() != 0 {
		t.Fatal("retry used validators from the failed response")
	}
	if got := w.ArticleRevision(); got != 1 {
		t.Fatalf("article revision after successful insert = %d, want 1", got)
	}
	if count, err := db.CountItems(storage.ItemFilter{}); err != nil || count != 2 {
		t.Fatalf("retried article count = %d, %v, want 2", count, err)
	}
	if count, err := db.CountItems(storage.ItemFilter{Search: "Hello"}); err != nil || count != 1 {
		t.Fatalf("retried search count = %d, %v, want 1", count, err)
	}
	updated, err = db.GetFeed(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastRefreshed == nil || updated.ETag == nil || updated.LastModified == nil || updated.LastError != nil {
		t.Fatalf("successful retry did not record its validators: %+v", updated)
	}
	w.RefreshFeed(feed.ID)
	if conditionalRequests.Load() != 1 {
		t.Fatal("next refresh did not use the stored validators")
	}
	if got := w.ArticleRevision(); got != 1 {
		t.Fatalf("article revision after 304 = %d, want 1", got)
	}
}

func TestRefreshFeedNoDuplicates(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	w := New(db)

	w.RefreshFeed(feed.ID)
	w.RefreshFeed(feed.ID)

	count, _ := db.CountItems(storage.ItemFilter{})
	if count != 2 {
		t.Errorf("after 2 refreshes count = %d, want 2 (no duplicates)", count)
	}
	if got := w.ArticleRevision(); got != 1 {
		t.Errorf("article revision after reading the same articles twice = %d, want 1", got)
	}
}

func TestRefreshFeedSkipsItemsOlderThanMaximumAge(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetSetting("item_max_age_days", "30"); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	body := fmt.Sprintf(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>Age test</title><link>https://example.com</link>
<item><guid>recent</guid><title>Recent</title><link>https://example.com/recent</link><pubDate>%s</pubDate></item>
<item><guid>old</guid><title>Old</title><link>https://example.com/old</link><pubDate>%s</pubDate></item>
</channel></rss>`, now.Add(-time.Hour).Format(time.RFC1123Z), now.AddDate(0, 0, -31).Format(time.RFC1123Z))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	New(db).RefreshFeed(feed.ID)

	items, err := db.ListItems(storage.ItemFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "recent" {
		t.Fatalf("stored items = %#v, want only recent item", items)
	}
}

func TestRefreshFeedDoesNotDuplicateExistingItemByLink(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0"><channel><title>Link test</title><link>https://example.com</link>
<item><title>Revised title</title><link>https://example.com/article</link><pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate></item>
</channel></rss>`)
	}))
	defer srv.Close()

	feed, _ := db.CreateFeed(srv.URL+"/feed.xml", nil)
	if err := db.CreateItems([]storage.Item{{
		FeedID: feed.ID,
		GUID:   "legacy-title-based-guid",
		Title:  "Original title",
		Link:   "https://example.com/article",
		Date:   time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}

	New(db).RefreshFeed(feed.ID)
	count, err := db.CountItems(storage.ItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("item count = %d, want 1", count)
	}
}

func TestFaviconSourceURL(t *testing.T) {
	tests := []struct {
		name                         string
		parsedSiteURL, storedSiteURL string
		feedURL                      string
		want                         string
	}{
		{"parsed site URL", "https://parsed.example", "https://stored.example", "https://feed.example/feed.xml", "https://parsed.example"},
		{"stored site URL", "", "https://stored.example", "https://feed.example/feed.xml", "https://stored.example"},
		{"feed URL", "", "", "https://feed.example/feed.xml", "https://feed.example/feed.xml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := faviconSourceURL(tt.parsedSiteURL, tt.storedSiteURL, tt.feedURL); got != tt.want {
				t.Errorf("faviconSourceURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- RefreshAll ---

func TestRefreshAll(t *testing.T) {
	db := newTestDB(t)
	srv := newFeedServer(t)

	db.CreateFeed(srv.URL+"/feed.xml", nil)
	db.CreateFeed(srv.URL+"/feed2.xml", nil) // same server, different path (still valid RSS)

	w := New(db)
	w.RefreshAll()

	count, _ := db.CountItems(storage.ItemFilter{})
	if count < 2 {
		t.Errorf("count = %d, want at least 2", count)
	}
}

func TestRefreshQueueSharesSlotsAndCoalescesFeeds(t *testing.T) {
	db := newTestDB(t)
	const feedCount = concurrentFeeds + 2
	requests := make(chan string, 2*feedCount)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		<-release
		fmt.Fprintf(w, rssSample, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
	}))
	defer srv.Close()
	releaseRequests := sync.OnceFunc(func() { close(release) })
	defer releaseRequests()
	feeds := make([]*storage.Feed, feedCount)
	for i := range feeds {
		feeds[i] = createFeedWithIcon(t, db, fmt.Sprintf("%s/feed/%d", srv.URL, i))
	}

	w := New(db)
	allDone := make(chan struct{})
	go func() {
		w.RefreshAll()
		close(allDone)
	}()
	seen := make(map[string]bool)
	receive := func() {
		t.Helper()
		select {
		case path := <-requests:
			if seen[path] {
				t.Fatalf("duplicate request to %s", path)
			}
			seen[path] = true
		case <-time.After(5 * time.Second):
			t.Fatal("no feed request received")
		}
	}
	for range concurrentFeeds {
		receive()
	}
	for _, feed := range feeds {
		w.RefreshFeedAsync(feed.ID)
	}

	w.refreshMu.Lock()
	queued := append([]feedRefresh(nil), w.queue...)
	workers, pending := w.workers, len(w.refreshes)
	w.refreshMu.Unlock()
	if workers != concurrentFeeds || len(queued) != feedCount-concurrentFeeds || pending != feedCount {
		t.Fatalf("refreshes active=%d, queued=%d, pending=%d, want %d, %d, %d", workers, len(queued), pending, concurrentFeeds, feedCount-concurrentFeeds, feedCount)
	}
	last := queued[len(queued)-1]
	if done := w.enqueueRefresh(last.id, true); done != last.done {
		t.Fatal("synchronous refresh did not share the queued async refresh")
	}
	if done := w.enqueueRefresh(queued[0].id, true); done != queued[0].done {
		t.Fatal("second synchronous refresh did not share the queued async refresh")
	}
	w.refreshMu.Lock()
	prioritized := w.queue[0].id
	w.refreshMu.Unlock()
	if prioritized != last.id {
		t.Fatal("synchronous refreshes did not preserve priority order")
	}
	releaseRequests()
	for range feedCount - concurrentFeeds {
		receive()
	}
	select {
	case <-allDone:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh all did not finish")
	}
	if len(requests) != 0 {
		t.Fatal("extra HTTP requests after the shared refreshes completed")
	}
	if count, err := db.CountItems(storage.ItemFilter{}); err != nil || count != 2*feedCount {
		t.Fatalf("article count = %d, %v, want %d", count, err, 2*feedCount)
	}
}

func TestRefreshStopInterruptsActiveRequest(t *testing.T) {
	db := newTestDB(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()
	feed := createFeedWithIcon(t, db, srv.URL)
	w := New(db)
	defer w.Stop()
	result := make(chan error, 1)
	go func() { result <- w.RefreshFeedContext(t.Context(), feed.ID) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("feed request did not start")
	}
	w.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("refresh returned %v, want ErrStopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not release its waiter")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("outgoing feed request was not canceled")
	}
}

func createFeedWithIcon(t *testing.T, db *storage.Storage, url string) *storage.Feed {
	t.Helper()
	feed, err := db.CreateFeed(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	icon := "test icon"
	if err := db.UpdateFeedIcon(feed.ID, &icon); err != nil {
		t.Fatal(err)
	}
	return feed
}

// --- Favicon ---

func TestFetchFavicon(t *testing.T) {
	// Serve a minimal PNG (1x1 pixel)
	png1x1 := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND chunk
		0x44, 0xae, 0x42, 0x60, 0x82,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><head><link rel="icon" href="/icon.png"></head></html>`)
		case "/icon.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png1x1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataURL, err := FetchFavicon(srv.URL)
	if err != nil {
		t.Fatalf("FetchFavicon: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Errorf("dataURL = %q, want data:image/png;base64,... prefix", dataURL[:40])
	}
}

func TestFetchFaviconFallback(t *testing.T) {
	// No <link rel="icon"> in HTML — should fall back to /favicon.ico
	png1x1 := []byte{0x89, 0x50, 0x4e, 0x47} // just the header for test

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><head><title>No icon link</title></head></html>`)
		case "/favicon.ico":
			w.Header().Set("Content-Type", "image/x-icon")
			w.Write(png1x1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataURL, err := FetchFavicon(srv.URL)
	if err != nil {
		t.Fatalf("FetchFavicon fallback: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/x-icon;base64,") {
		t.Errorf("dataURL prefix wrong: %q", dataURL[:40])
	}
}

func TestFetchFaviconTriesLaterIconCandidate(t *testing.T) {
	png1x1 := []byte{0x89, 0x50, 0x4e, 0x47}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="icon" href="/missing.png"><link rel="icon" href="/icon.png"></head></html>`)
		case "/missing.png":
			http.NotFound(w, r)
		case "/icon.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png1x1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataURL, err := FetchFavicon(srv.URL)
	if err != nil {
		t.Fatalf("FetchFavicon: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Errorf("dataURL = %q, want data:image/png;base64,... prefix", dataURL[:40])
	}
}

func TestFetchFaviconUnsupportedType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><head></head></html>`)
		case "/favicon.ico":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "not an image")
		}
	}))
	defer srv.Close()

	_, err := FetchFavicon(srv.URL)
	if err == nil {
		t.Error("FetchFavicon should return error for unsupported MIME type")
	}
}

func TestFetchFaviconRejectsEmptyIcon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head></head></html>`)
		case "/favicon.ico":
			w.Header().Set("Content-Type", "image/x-icon")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := FetchFavicon(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "empty icon response") {
		t.Fatalf("FetchFavicon error = %v, want empty icon response", err)
	}
}

// --- refreshInterval ---

func TestRefreshIntervalDefault(t *testing.T) {
	db := newTestDB(t)
	w := New(db)

	d := w.refreshInterval()
	if d != defaultInterval {
		t.Errorf("refreshInterval = %v, want %v", d, defaultInterval)
	}
}

func TestRefreshIntervalFromSettings(t *testing.T) {
	db := newTestDB(t)
	db.SetSetting("refresh_rate", "15")

	w := New(db)
	d := w.refreshInterval()
	if d != 15*time.Minute {
		t.Errorf("refreshInterval = %v, want 15m", d)
	}
}

func TestItemMaxAgeDaysDefaultsTo30(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetSetting("item_max_age_days", ""); err != nil {
		t.Fatal(err)
	}
	if got := New(db).itemMaxAgeDays(); got != 30 {
		t.Errorf("itemMaxAgeDays = %d, want 30", got)
	}
}

func TestItemMaxAgeDaysAllowsUnlimited(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetSetting("item_max_age_days", "0"); err != nil {
		t.Fatal(err)
	}
	if got := New(db).itemMaxAgeDays(); got != 0 {
		t.Errorf("itemMaxAgeDays = %d, want 0", got)
	}
}

func TestRefreshIntervalDisabled(t *testing.T) {
	db := newTestDB(t)
	db.SetSetting("refresh_rate", "0")

	w := New(db)
	d := w.refreshInterval()
	if d != 0 {
		t.Errorf("refreshInterval = %v, want 0 (disabled)", d)
	}
}
