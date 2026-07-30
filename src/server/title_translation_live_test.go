//go:build live_henji

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"forge.harakara.site/littleisland/hayari/src/storage"
	"forge.harakara.site/littleisland/hayari/src/titletranslation"
)

// TestLiveHenjiTitleTranslation exercises the real Henji command and its
// configured provider. It is deliberately opt-in because it sends three
// non-secret English titles to the provider and can incur usage charges.
func TestLiveHenjiTitleTranslation(t *testing.T) {
	if os.Getenv("HAYARI_LIVE_HENJI_TEST") != "1" {
		t.Skip("set HAYARI_LIVE_HENJI_TEST=1 to call the configured Henji provider")
	}
	path := os.Getenv("HAYARI_HENJI_PATH")
	if path == "" {
		path = "/usr/local/bin/henji"
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Henji executable %q: %v", path, err)
	}

	srv, ts := newTestServer(t)
	srv.translations = titletranslation.New(srv.db, titletranslation.Config{
		Path: path, API: "openrouter", Model: "google/gemini-2.5-flash-lite",
	})
	t.Cleanup(func() { srv.Stop() })

	english := seedLiveFeed(t, srv.db, "live-english", "A practical guide to RSS readers")
	assertLiveStart(t, ts, english.feed.ID, 1)
	englishItems := waitForLiveStates(t, srv.db, english.feed.ID, 1)
	if englishItems[0].TitleTranslationState != storage.TitleTranslationTranslated || englishItems[0].TranslatedTitle == nil || *englishItems[0].TranslatedTitle == "" {
		t.Fatalf("English translation = %#v, want translated non-empty title", englishItems[0])
	}

	// No Latin letters: this must be classified locally and make no provider call.
	japanese := seedLiveFeed(t, srv.db, "live-japanese", "これは日本語だけのタイトルです")
	assertLiveStart(t, ts, japanese.feed.ID, 1)
	japaneseItems := waitForLiveStates(t, srv.db, japanese.feed.ID, 1)
	if japaneseItems[0].TitleTranslationState != storage.TitleTranslationSkipped || japaneseItems[0].TranslatedTitle != nil {
		t.Fatalf("Japanese title = %#v, want skipped with no translation", japaneseItems[0])
	}

	// A single job processes multiple titles sequentially.
	batch := seedLiveFeed(t, srv.db, "live-batch-a", "Understanding Atom feeds")
	if err := srv.db.CreateItems([]storage.Item{{
		FeedID: batch.feed.ID, GUID: "live-batch-b", Title: "Why feed readers still matter", Date: time.Now().Add(time.Second),
	}}); err != nil {
		t.Fatal(err)
	}
	assertLiveStart(t, ts, batch.feed.ID, 2)
	batchItems := waitForLiveStates(t, srv.db, batch.feed.ID, 2)
	for _, item := range batchItems {
		if item.TitleTranslationState != storage.TitleTranslationTranslated || item.TranslatedTitle == nil || *item.TranslatedTitle == "" {
			t.Fatalf("batch translation = %#v, want translated non-empty title", item)
		}
	}
}

type liveFeed struct{ feed *storage.Feed }

func seedLiveFeed(t *testing.T, db *storage.Storage, guid, title string) liveFeed {
	t.Helper()
	feed, err := db.CreateFeed("https://example.test/"+guid+".xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateItems([]storage.Item{{FeedID: feed.ID, GUID: guid, Title: title, Date: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	return liveFeed{feed: feed}
}

func assertLiveStart(t *testing.T, ts *httptest.Server, feedID int64, wantAccepted int) {
	t.Helper()
	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/feeds/%d/title-translations", feedID), "")
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("start status = %d, want 202", resp.StatusCode)
	}
	var body struct {
		Accepted int `json:"accepted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Accepted != wantAccepted {
		t.Fatalf("accepted = %d, want %d", body.Accepted, wantAccepted)
	}
}

func waitForLiveStates(t *testing.T, db *storage.Storage, feedID int64, want int) []storage.Item {
	t.Helper()
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		items, err := db.ListItems(storage.ItemFilter{FeedID: &feedID}, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == want {
			complete := true
			for _, item := range items {
				complete = complete && item.TitleTranslationState != storage.TitleTranslationProcessing && item.TitleTranslationState != storage.TitleTranslationPending
			}
			if complete {
				return items
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("translation did not finish for feed %d", feedID)
	return nil
}
