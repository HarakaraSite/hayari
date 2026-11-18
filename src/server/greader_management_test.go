package server

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFreshRSSFolderRenameAndDelete(t *testing.T) {
	srv, ts := newTestServer(t)
	token := loginAt(t, ts, freshRSSGReaderPrefix+"/accounts/ClientLogin")
	const oldTitle = "仕事 + 開発"
	const newTitle = "資料 & 経営"
	const label = "user/-/label/"
	base := freshRSSGReaderPrefix + "/reader/api/0"
	feed := seedFeed(t, srv.db, "https://example.test/folder.xml", oldTitle)
	article := seedItems(t, srv.db, feed.ID, 1)[0]

	other, err := srv.db.CreateFolder("other")
	if err != nil {
		t.Fatal(err)
	}
	rename := func(from, to string) *http.Response {
		return grPost(t, ts, token, base+"/rename-tag", url.Values{"s": {from}, "dest": {to}})
	}
	conflict := rename(label+oldTitle, label+other.Title)
	conflict.Body.Close()
	if conflict.StatusCode != http.StatusBadRequest {
		t.Fatalf("rename to existing folder status = %d, want 400", conflict.StatusCode)
	}

	requireGReaderOK(t, rename(label+oldTitle, label+newTitle))
	folder, err := srv.db.GetFolderByTitle(newTitle)
	if err != nil || folder.ID != *feed.FolderID {
		t.Fatalf("renamed folder = %+v, %v, want same ID %d", folder, err, *feed.FolderID)
	}

	requireGReaderOK(t, grPost(t, ts, token, base+"/disable-tag", url.Values{"s": {label + newTitle}}))
	if _, err := srv.db.GetFolder(folder.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted folder lookup error = %v, want not found", err)
	}
	gotFeed, err := srv.db.GetFeed(feed.ID)
	if err != nil || gotFeed.FolderID != nil {
		t.Fatalf("feed after deleting folder = %+v, %v, want uncategorized", gotFeed, err)
	}
	gotItem, err := srv.db.GetItem(article.ID)
	if err != nil || gotItem.FeedID != feed.ID {
		t.Fatalf("article after deleting folder = %+v, %v, want original feed %d", gotItem, err, feed.ID)
	}

	// The Web UI can create two folders with the same name. The label alone
	// cannot identify which one the client wants to delete.
	duplicate, err := srv.db.CreateFolder(other.Title)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := grPost(t, ts, token, base+"/disable-tag", url.Values{"s": {label + other.Title}})
	ambiguous.Body.Close()
	if ambiguous.StatusCode != http.StatusBadRequest {
		t.Fatalf("ambiguous folder deletion status = %d, want 400", ambiguous.StatusCode)
	}
	for _, id := range []int64{other.ID, duplicate.ID} {
		if _, err := srv.db.GetFolder(id); err != nil {
			t.Fatalf("folder %d after ambiguous deletion: %v", id, err)
		}
	}
}

func TestFreshRSSSubscriptionImport(t *testing.T) {
	srv, ts := newTestServer(t)
	token := loginAt(t, ts, freshRSSGReaderPrefix+"/accounts/ClientLogin")
	path := ts.URL + freshRSSGReaderPrefix + "/reader/api/0/subscription/import"
	const feedURL = "http://127.0.0.1:1/feed.xml"
	const document = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><body><outline text="Test folder">
  <outline type="rss" text="Test Feed" xmlUrl="` + feedURL + `" />
</outline></body></opml>`

	post := func(body, auth string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "text/xml")
		if auth != "" {
			req.Header.Set("Authorization", "GoogleLogin auth="+auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for _, tc := range []struct {
		body string
		auth string
		want int
	}{
		{document, "", http.StatusUnauthorized},
		{"<opml>", token, http.StatusBadRequest},
	} {
		resp := post(tc.body, tc.auth)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("import status = %d, want %d", resp.StatusCode, tc.want)
		}
	}
	requireGReaderOK(t, post(document, token))
	requireGReaderOK(t, post(document, token))

	folders, err := srv.db.ListFolders()
	if err != nil || len(folders) != 1 || folders[0].Title != "Test folder" {
		t.Fatalf("imported folders = %+v, %v, want Test folder once", folders, err)
	}
	feeds, err := srv.db.ListFeeds()
	if err != nil || len(feeds) != 1 {
		t.Fatalf("imported feeds = %+v, %v, want one feed", feeds, err)
	}
	feed := feeds[0]
	if feed.FeedURL != feedURL || feed.Title != "Test Feed" || feed.FolderID == nil || *feed.FolderID != folders[0].ID {
		t.Fatalf("imported feed = %+v, want %s in folder %d", feed, feedURL, folders[0].ID)
	}
}

func requireGReaderOK(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != "OK" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("response = %d %s %q, %v, want 200 text/plain OK", resp.StatusCode, resp.Header.Get("Content-Type"), body, err)
	}
}
