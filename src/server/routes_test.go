package server

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

// Helpers reuse newTestServer and buildMux from greader_test.go.

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func doRequest(t *testing.T, ts *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHealthz(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/healthz", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body["ok"] {
		t.Errorf("response = %v, want ok=true", body)
	}
}

func TestTitleTranslationCapabilityAndUnavailableStart(t *testing.T) {
	srv, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/api/capabilities", "")
	defer resp.Body.Close()
	var capability struct {
		TitleTranslation bool `json:"title_translation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&capability); err != nil {
		t.Fatal(err)
	}
	if capability.TitleTranslation {
		t.Fatal("test server must not advertise missing henji")
	}
	feed := seedFeed(t, srv.db, "https://example.com/unavailable.xml", "")
	resp = doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/feeds/%d/title-translations", feed.ID), "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("start unavailable = %d, want 204", resp.StatusCode)
	}
	resp = doRequest(t, ts, http.MethodPost, "/api/feeds/999999/title-translations", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing feed = %d, want 404", resp.StatusCode)
	}
}

func TestStaticAssetsAreNotCached(t *testing.T) {
	_, ts := newTestServer(t)
	for _, path := range []string{"/", "/favicon.svg", "/hayari-mark.svg", "/stylesheets/pico.min.css", "/stylesheets/app.css", "/javascripts/api.js", "/javascripts/key.js", "/javascripts/app.js"} {
		resp := doRequest(t, ts, http.MethodGet, path, "")
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("%s: status = %d, want 200", path, resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			resp.Body.Close()
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
			continue
		}
		resp.Body.Close()
	}
}

func TestFaviconIsPublic(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Username = "user"
	srv.Password = "pass"

	resp := doRequest(t, ts, http.MethodGet, "/favicon.svg", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
}

func TestStatsIncludesStarredCounts(t *testing.T) {
	srv, ts := newTestServer(t)
	feed := seedFeed(t, srv.db, "https://example.com/feed.xml", "")
	items := seedItems(t, srv.db, feed.ID, 2)
	if err := srv.db.SetStarred(items[0].ID, true); err != nil {
		t.Fatal(err)
	}

	resp := doRequest(t, ts, http.MethodGet, "/api/stats", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var stats struct {
		Unread  map[int64]int64 `json:"unread"`
		Starred map[int64]int64 `json:"starred"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if got := stats.Unread[feed.ID]; got != 2 {
		t.Errorf("unread count = %d, want 2", got)
	}
	if got := stats.Starred[feed.ID]; got != 1 {
		t.Errorf("starred count = %d, want 1", got)
	}
}

func TestIndexUsesUnversionedLocalAssets(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/", "")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, asset := range []string{"/hayari-mark.svg", "/stylesheets/pico.min.css", "/stylesheets/app.css", "/javascripts/api.js", "/javascripts/key.js", "/javascripts/app.js"} {
		if !strings.Contains(page, asset) {
			t.Errorf("index does not reference %s", asset)
		}
	}
	if strings.Contains(page, "?v=") {
		t.Error("index must not use manual asset version query parameters")
	}
	if strings.Contains(page, "https://") {
		t.Error("index must not reference external assets")
	}
}

func TestLoginUsesLocalPicoCSS(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/login", "")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if !strings.Contains(page, "/stylesheets/pico.min.css") {
		t.Error("login does not reference the bundled Pico CSS")
	}
	if strings.Contains(page, "https://") {
		t.Error("login must not reference external assets")
	}
}

func TestHTTPServerTimeouts(t *testing.T) {
	srv, _ := newTestServer(t)
	httpServer := srv.newHTTPServer(http.NewServeMux())
	if httpServer.ReadHeaderTimeout != readHeaderTimeout ||
		httpServer.ReadTimeout != readTimeout ||
		httpServer.WriteTimeout != writeTimeout ||
		httpServer.IdleTimeout != idleTimeout {
		t.Fatalf("unexpected timeouts: %#v", httpServer)
	}
}

func TestIsLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7070", "[::1]:7070", "localhost:7070"} {
		if !isLoopbackAddress(addr) {
			t.Errorf("%q should be loopback", addr)
		}
	}
	for _, addr := range []string{":7070", "0.0.0.0:7070", "192.168.1.2:7070"} {
		if isLoopbackAddress(addr) {
			t.Errorf("%q should not be loopback", addr)
		}
	}
}

func TestHealthzMethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodPost, "/healthz", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestStatusUsesServerVersion(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Version = "v1.2.3"
	resp := doRequest(t, ts, http.MethodGet, "/api/status", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "v1.2.3" {
		t.Errorf("version = %q, want %q", body.Version, "v1.2.3")
	}
}

// --- GET /api/feeds/errors ---

func TestFeedErrorsEmpty(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/api/feeds/errors", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestFeedErrorsPopulated(t *testing.T) {
	srv, ts := newTestServer(t)

	feed, _ := srv.db.CreateFeed("http://example.com/feed.rss", nil)
	srv.db.UpdateFeedError(feed.ID, "connection refused")

	resp := doRequest(t, ts, http.MethodGet, "/api/feeds/errors", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Response is map[int64]string — JSON keys are always strings
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%d", feed.ID)
	if result[key] != "connection refused" {
		t.Errorf("errors[%s] = %q, want %q", key, result[key], "connection refused")
	}
}

// --- GET /api/feeds/:id/icon ---

func TestFeedIconNotFound(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/api/feeds/999/icon", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFeedIconServed(t *testing.T) {
	srv, ts := newTestServer(t)

	feed, _ := srv.db.CreateFeed("http://example.com/feed.rss", nil)

	// Store a 1-pixel PNG as base64 data URL
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	dataURL := "data:image/png;base64," + pngBase64
	srv.db.UpdateFeedIcon(feed.ID, &dataURL)

	resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/feeds/%d/icon", feed.ID), "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if etag := resp.Header.Get("ETag"); etag == "" {
		t.Error("ETag header should be set")
	}
}

func TestFeedIconETagNotModified(t *testing.T) {
	srv, ts := newTestServer(t)

	feed, _ := srv.db.CreateFeed("http://example.com/feed.rss", nil)
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	dataURL := "data:image/png;base64," + pngBase64
	srv.db.UpdateFeedIcon(feed.ID, &dataURL)

	// First request to get ETag
	resp1 := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/feeds/%d/icon", feed.ID), "")
	etag := resp1.Header.Get("ETag")
	resp1.Body.Close()

	// Second request with If-None-Match
	req, _ := http.NewRequest(http.MethodGet, ts.URL+fmt.Sprintf("/api/feeds/%d/icon", feed.ID), nil)
	req.Header.Set("If-None-Match", etag)
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", resp2.StatusCode)
	}
}

// --- PUT /api/folders/:id with is_expanded ---

func TestFolderUpdateIsExpanded(t *testing.T) {
	srv, ts := newTestServer(t)

	folder, _ := srv.db.CreateFolder("Tech")
	if folder.IsExpanded {
		t.Fatal("folder should not be expanded initially")
	}

	resp := doRequest(t, ts, http.MethodPut,
		fmt.Sprintf("/api/folders/%d", folder.ID),
		`{"is_expanded": true}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	updated, _ := srv.db.GetFolder(folder.ID)
	if !updated.IsExpanded {
		t.Error("IsExpanded should be true after PUT")
	}
}

func TestFolderUpdateTitleAndIsExpanded(t *testing.T) {
	srv, ts := newTestServer(t)

	folder, _ := srv.db.CreateFolder("Old Name")

	resp := doRequest(t, ts, http.MethodPut,
		fmt.Sprintf("/api/folders/%d", folder.ID),
		`{"title": "New Name", "is_expanded": true}`)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	updated, _ := srv.db.GetFolder(folder.ID)
	if updated.Title != "New Name" {
		t.Errorf("Title = %q, want %q", updated.Title, "New Name")
	}
	if !updated.IsExpanded {
		t.Error("IsExpanded should be true")
	}
}

// --- OPML import ---

const sampleOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.1">
  <head><title>Test</title></head>
  <body>
    <outline text="Tech" title="Tech">
      <outline text="Go Blog" title="Go Blog" type="rss"
               xmlUrl="http://example.com/go.xml" htmlUrl="http://example.com"/>
    </outline>
    <outline text="Hacker News" type="rss"
             xmlUrl="http://example.com/hn.xml" htmlUrl="https://news.ycombinator.com"/>
  </body>
</opml>`

func TestOPMLImport(t *testing.T) {
	srv, ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/opml/import", strings.NewReader(sampleOPML))
	req.Header.Set("Content-Type", "application/xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var result map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["imported"] != 2 {
		t.Errorf("imported = %d, want 2", result["imported"])
	}

	// Folder should have been created
	folder, err := srv.db.GetFolderByTitle("Tech")
	if err != nil {
		t.Fatalf("folder 'Tech' not found: %v", err)
	}

	// Check feeds
	feeds, _ := srv.db.ListFeeds()
	if len(feeds) != 2 {
		t.Errorf("feed count = %d, want 2", len(feeds))
	}

	// "Go Blog" feed should be in the Tech folder
	var goFeed *storage.Feed
	for i := range feeds {
		if feeds[i].FeedURL == "http://example.com/go.xml" {
			goFeed = &feeds[i]
			break
		}
	}
	if goFeed == nil {
		t.Fatal("Go Blog feed not found")
	}
	if goFeed.FolderID == nil || *goFeed.FolderID != folder.ID {
		t.Errorf("Go Blog feed.FolderID = %v, want %d", goFeed.FolderID, folder.ID)
	}
}

// --- OPML export ---

func TestOPMLExport(t *testing.T) {
	srv, ts := newTestServer(t)

	// Set up feeds and folder
	folder, _ := srv.db.CreateFolder("Tech")
	srv.db.CreateFeed("http://example.com/go.xml", &folder.ID)
	srv.db.CreateFeed("http://example.com/hn.xml", nil)

	resp := doRequest(t, ts, http.MethodGet, "/opml/export", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type = %q, want application/xml", ct)
	}

	body, _ := io.ReadAll(resp.Body)

	var doc opmlDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v\nbody:\n%s", err, body)
	}

	// Count all feeds across all outlines
	feedCount := 0
	for _, o := range doc.Body.Outlines {
		if o.XMLUrl != "" {
			feedCount++
		} else {
			feedCount += len(o.Outlines)
		}
	}
	if feedCount != 2 {
		t.Errorf("exported feed count = %d, want 2", feedCount)
	}
}

// --- GET /page ---

func TestHandlePage(t *testing.T) {
	previousClient := pageClient
	pageClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html><body><p>Hello page</p></body></html>")),
			Header:     make(http.Header),
		}, nil
	})}
	defer func() { pageClient = previousClient }()

	_, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/page?url=https://example.com/article", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result["content"], "Hello page") {
		t.Errorf("page content missing expected text, got: %q", result["content"])
	}
}

func TestHandlePageMissingURL(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/page", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandlePageRejectsPrivateAddress(t *testing.T) {
	_, ts := newTestServer(t)
	resp := doRequest(t, ts, http.MethodGet, "/page?url=http://127.0.0.1:1/", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- /api/settings (whitelist) ---

func TestSettingsGetExcludesAuthSecret(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.db.SetSetting("auth_secret", "deadbeef")
	srv.db.SetSetting("theme", "dark")

	resp := doRequest(t, ts, http.MethodGet, "/api/settings", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["auth_secret"]; ok {
		t.Fatal("auth_secret exposed via GET /api/settings")
	}
	if result["theme"] != "dark" {
		t.Fatalf("theme = %q, want dark", result["theme"])
	}
}

func TestSettingsPutRejectsAuthSecret(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.db.SetSetting("auth_secret", "original")

	resp := doRequest(t, ts, http.MethodPut, "/api/settings", `{"auth_secret":"evil"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	val, _ := srv.db.GetSetting("auth_secret")
	if val != "original" {
		t.Fatalf("auth_secret overwritten: %q", val)
	}
}

func TestSettingsPutAllowsWhitelistedKeys(t *testing.T) {
	srv, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodPut, "/api/settings", `{"theme":"beige","font_size":"large","refresh_rate":"30"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if val, _ := srv.db.GetSetting("theme"); val != "beige" {
		t.Fatalf("theme = %q, want beige", val)
	}

	resp = doRequest(t, ts, http.MethodGet, "/api/settings", "")
	defer resp.Body.Close()
	var settings map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings["theme"] != "beige" {
		t.Fatalf("returned theme = %q, want beige", settings["theme"])
	}
}

// --- PUT /api/feeds/:id folder_id semantics ---

func TestFeedUpdateFolderNullClears(t *testing.T) {
	srv, ts := newTestServer(t)
	folder, _ := srv.db.CreateFolder("News")
	feed, _ := srv.db.CreateFeed("https://example.com/feed.xml", &folder.ID)

	// Explicit null moves the feed out of the folder
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/feeds/%d", feed.ID), `{"folder_id":null}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	got, _ := srv.db.GetFeed(feed.ID)
	if got.FolderID != nil {
		t.Fatal("folder_id:null did not clear folder")
	}
}

func TestFeedUpdateFolderAbsentKeeps(t *testing.T) {
	srv, ts := newTestServer(t)
	folder, _ := srv.db.CreateFolder("News")
	feed, _ := srv.db.CreateFeed("https://example.com/feed.xml", &folder.ID)

	// Title-only update must not touch the folder
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/feeds/%d", feed.ID), `{"title":"Renamed"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	got, _ := srv.db.GetFeed(feed.ID)
	if got.FolderID == nil || *got.FolderID != folder.ID {
		t.Fatal("absent folder_id key cleared the folder")
	}
	if got.Title != "Renamed" {
		t.Fatalf("title = %q, want Renamed", got.Title)
	}
}

func TestFeedUpdateTitleFilterKeywords(t *testing.T) {
	srv, ts := newTestServer(t)
	feed, _ := srv.db.CreateFeed("https://example.com/feed.xml", nil)

	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/feeds/%d", feed.ID), `{"title_filter_keywords":" ハンター, サッカー "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	updated, err := srv.db.GetFeed(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TitleFilterKeywords != "ハンター,サッカー" {
		t.Errorf("TitleFilterKeywords = %q, want normalized keywords", updated.TitleFilterKeywords)
	}
}

// --- GReader subscription/edit remove label ---

func TestGreaderEditRemoveLabel(t *testing.T) {
	srv, ts := newTestServer(t)
	folder, _ := srv.db.CreateFolder("News")
	feed, _ := srv.db.CreateFeed("https://example.com/feed.xml", &folder.ID)

	token := login(t, ts)
	resp := grPost(t, ts, token, "/reader/api/0/subscription/edit", url.Values{
		"ac": {"edit"},
		"s":  {fmt.Sprintf("feed/%d", feed.ID)},
		"r":  {"user/-/label/News"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := srv.db.GetFeed(feed.ID)
	if got.FolderID != nil {
		t.Fatal("remove label did not clear folder")
	}
}
