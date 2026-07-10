package worker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nkanaev/yarr2/src/storage"
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
	f, err := os.CreateTemp("", "yarr2-worker-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	s, err := storage.Open(f.Name())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
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

func TestRefreshIntervalDisabled(t *testing.T) {
	db := newTestDB(t)
	db.SetSetting("refresh_rate", "0")

	w := New(db)
	d := w.refreshInterval()
	if d != 0 {
		t.Errorf("refreshInterval = %v, want 0 (disabled)", d)
	}
}
