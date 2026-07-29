package worker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindFeedsUsesRSSUserAgentAndExtractsRelativeLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), UserAgent(); got != want {
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

func TestExtractFeedLinksRequiresFeedMIMEType(t *testing.T) {
	feeds, err := extractFeedLinks(strings.NewReader(`<!doctype html><head>
		<link rel="alternate" href="https://m.example.com/">
		<link rel="alternate" href="android-app://com.example/http/example.com/">
		<link rel="alternate" type="text/html" href="/translated">
		<link rel="alternate" type="application/json" href="/wp-json/">
		<link rel="alternate" type="Application/RSS+XML; charset=UTF-8" title="RSS" href="/rss.xml">
		<link type="application/atom+xml" title="Atom" href="/atom.xml">
		<link rel="alternate" type="application/feed+json" title="JSON Feed" href="/feed.json">
	</head>`), "https://example.com/news")
	if err != nil {
		t.Fatal(err)
	}
	want := []FoundFeed{
		{URL: "https://example.com/rss.xml", Title: "RSS"},
		{URL: "https://example.com/atom.xml", Title: "Atom"},
		{URL: "https://example.com/feed.json", Title: "JSON Feed"},
	}
	if len(feeds) != len(want) {
		t.Fatalf("feed count = %d, want %d: %#v", len(feeds), len(want), feeds)
	}
	for i := range want {
		if feeds[i] != want[i] {
			t.Errorf("feed[%d] = %#v, want %#v", i, feeds[i], want[i])
		}
	}
}

func TestExtractFeedLinksDoesNotTreatDirectAtomEntriesAsFeedCandidates(t *testing.T) {
	// A directly entered YouTube playlist RSS feed has an untyped self link
	// and article links with rel="alternate". Neither must replace the URL the
	// user entered; the UI falls back to subscribing to that URL when no
	// candidates are returned.
	feeds, err := extractFeedLinks(strings.NewReader(`<?xml version="1.0"?>
		<feed xmlns="http://www.w3.org/2005/Atom">
			<link rel="self" href="https://www.youtube.com/feeds/videos.xml?playlist_id=UULFexample"/>
			<entry>
				<link rel="alternate" href="https://www.youtube.com/watch?v=example"/>
			</entry>
		</feed>`), "https://www.youtube.com/feeds/videos.xml?playlist_id=UULFexample")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatalf("feed candidates = %#v, want none for a directly entered Atom feed", feeds)
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
