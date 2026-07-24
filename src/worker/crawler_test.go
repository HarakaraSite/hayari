package worker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindFeedsUsesRSSUserAgentAndExtractsRelativeLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), "yarr2/1.0 (+https://github.com/nkanaev/yarr2)"; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" title="Example feed" href="/feed.xml"></head></html>`))
	}))
	defer server.Close()

	feeds, err := findFeeds(server.Client(), server.URL+"/news")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("feed count = %d, want 1", len(feeds))
	}
	if got, want := feeds[0], (FoundFeed{URL: server.URL + "/feed.xml", Title: "Example feed"}); got != want {
		t.Errorf("feed = %#v, want %#v", got, want)
	}
}

func TestFindFeedsReturnsUpstreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := findFeeds(server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("error = %v, want HTTP 403", err)
	}
}
