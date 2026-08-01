package parser

import (
	"strings"
	"testing"
	"time"
)

// --- RSS 2.0 ---

const rss2Sample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test RSS Feed</title>
    <link>https://example.com</link>
    <item>
      <guid>https://example.com/post/1</guid>
      <title>First Post</title>
      <link>https://example.com/post/1</link>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
      <description>Short description</description>
      <content:encoded xmlns:content="http://purl.org/rss/1.0/modules/content/"><![CDATA[<p>Full content</p>]]></content:encoded>
      <author>alice@example.com (Alice)</author>
    </item>
  </channel>
</rss>`

func TestParseRSS2(t *testing.T) {
	feed, err := Parse([]byte(rss2Sample))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if feed.Title != "Test RSS Feed" {
		t.Errorf("Title = %q, want %q", feed.Title, "Test RSS Feed")
	}
	if feed.SiteURL != "https://example.com" {
		t.Errorf("SiteURL = %q, want %q", feed.SiteURL, "https://example.com")
	}
	if len(feed.Items) != 1 {
		t.Fatalf("Items count = %d, want 1", len(feed.Items))
	}
	item := feed.Items[0]
	if item.GUID != "https://example.com/post/1" {
		t.Errorf("GUID = %q, want %q", item.GUID, "https://example.com/post/1")
	}
	if item.Title != "First Post" {
		t.Errorf("Title = %q, want %q", item.Title, "First Post")
	}
	if item.Link != "https://example.com/post/1" {
		t.Errorf("Link = %q, want %q", item.Link, "https://example.com/post/1")
	}
	// content:encoded が優先されるべき
	if item.Content != "<p>Full content</p>" {
		t.Errorf("Content = %q, want %q", item.Content, "<p>Full content</p>")
	}
}

// --- Atom ---

const atomSample = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Atom Feed</title>
  <link href="https://atom.example.com"/>
  <entry>
    <id>urn:uuid:atom-entry-1</id>
    <title>Atom Entry</title>
    <link href="https://atom.example.com/entry/1"/>
    <published>2024-06-01T12:00:00Z</published>
    <author><name>Bob</name><email>bob@example.com</email></author>
    <summary>Summary text</summary>
    <content type="html">&lt;p&gt;Atom content&lt;/p&gt;</content>
  </entry>
</feed>`

func TestParseAtom(t *testing.T) {
	feed, err := Parse([]byte(atomSample))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if feed.Title != "Test Atom Feed" {
		t.Errorf("Title = %q, want %q", feed.Title, "Test Atom Feed")
	}
	if len(feed.Items) != 1 {
		t.Fatalf("Items count = %d, want 1", len(feed.Items))
	}
	item := feed.Items[0]
	if item.GUID != "urn:uuid:atom-entry-1" {
		t.Errorf("GUID = %q, want %q", item.GUID, "urn:uuid:atom-entry-1")
	}
	if item.Author != "Bob bob@example.com" {
		t.Errorf("Author = %q, want %q", item.Author, "Bob bob@example.com")
	}
	want := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	if !item.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", item.Date, want)
	}
}

// --- JSON Feed ---

const jsonFeedSample = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "Test JSON Feed",
  "home_page_url": "https://json.example.com",
  "items": [
    {
      "id": "json-item-1",
      "title": "JSON Item",
      "url": "https://json.example.com/item/1",
      "date_published": "2024-03-15T09:00:00Z",
      "content_html": "<p>JSON content</p>",
      "authors": [{"name": "Carol"}]
    }
  ]
}`

func TestParseJSONFeed(t *testing.T) {
	feed, err := Parse([]byte(jsonFeedSample))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if feed.Title != "Test JSON Feed" {
		t.Errorf("Title = %q, want %q", feed.Title, "Test JSON Feed")
	}
	if len(feed.Items) != 1 {
		t.Fatalf("Items count = %d, want 1", len(feed.Items))
	}
	item := feed.Items[0]
	if item.GUID != "json-item-1" {
		t.Errorf("GUID = %q, want %q", item.GUID, "json-item-1")
	}
	if item.Content != "<p>JSON content</p>" {
		t.Errorf("Content = %q, want %q", item.Content, "<p>JSON content</p>")
	}
}

// --- GUID フォールバック ---

const rssNoGUID = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>No GUID Feed</title>
    <item>
      <title>No GUID Item</title>
      <link>https://example.com/no-guid</link>
    </item>
    <item>
      <title>No GUID Item</title>
      <link>https://example.com/no-guid</link>
    </item>
  </channel>
</rss>`

func TestGUIDFallback(t *testing.T) {
	feed, err := Parse([]byte(rssNoGUID))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(feed.Items) < 1 {
		t.Fatal("no items parsed")
	}
	guid := feed.Items[0].GUID
	if guid == "" {
		t.Error("GUID should not be empty when guid element is missing")
	}
	// 同じリンクなら同じ GUID になる。タイトルが更新されても変わらない。
	if len(feed.Items) >= 2 && feed.Items[0].GUID != feed.Items[1].GUID {
		t.Error("identical links should produce the same fallback GUID")
	}
}

func TestGUIDFallbackIgnoresTitleChanges(t *testing.T) {
	first, err := Parse([]byte(strings.Replace(rssNoGUID, "No GUID Item", "Original title", 1)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse([]byte(strings.Replace(rssNoGUID, "No GUID Item", "Revised title", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].GUID != second.Items[0].GUID {
		t.Errorf("fallback GUID changed after title revision: %q != %q", first.Items[0].GUID, second.Items[0].GUID)
	}
}

// --- 日時フォールバック ---

const rssNoDate = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>No Date Feed</title>
    <item>
      <guid>no-date-1</guid>
      <title>No Date</title>
    </item>
  </channel>
</rss>`

func TestDateFallbackToNow(t *testing.T) {
	before := time.Now().Add(-time.Second)
	feed, err := Parse([]byte(rssNoDate))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	after := time.Now().Add(time.Second)
	if len(feed.Items) == 0 {
		t.Fatal("no items")
	}
	d := feed.Items[0].Date
	if d.Before(before) || d.After(after) {
		t.Errorf("Date %v should be close to now (between %v and %v)", d, before, after)
	}
}

// --- Content フォールバック（description → content）---

const rssDescriptionOnly = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Desc Feed</title>
    <item>
      <guid>desc-1</guid>
      <title>Desc Only</title>
      <description>Only description here</description>
    </item>
  </channel>
</rss>`

func TestContentFallbackToDescription(t *testing.T) {
	feed, err := Parse([]byte(rssDescriptionOnly))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(feed.Items) == 0 {
		t.Fatal("no items")
	}
	if feed.Items[0].Content != "Only description here" {
		t.Errorf("Content = %q, want %q", feed.Items[0].Content, "Only description here")
	}
}

// --- Author（名前のみ / メールのみ / 両方）---

const atomAuthorVariants = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Author Test</title>
  <entry>
    <id>a1</id><title>Name only</title>
    <author><name>Dave</name></author>
  </entry>
  <entry>
    <id>a2</id><title>Email only</title>
    <author><email>eve@example.com</email></author>
  </entry>
  <entry>
    <id>a3</id><title>Both</title>
    <author><name>Frank</name><email>frank@example.com</email></author>
  </entry>
  <entry>
    <id>a4</id><title>No author</title>
  </entry>
</feed>`

func TestAuthorVariants(t *testing.T) {
	feed, err := Parse([]byte(atomAuthorVariants))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cases := []struct {
		idx  int
		want string
	}{
		{0, "Dave"},
		{1, "eve@example.com"},
		{2, "Frank frank@example.com"},
		{3, ""},
	}
	for _, c := range cases {
		got := feed.Items[c.idx].Author
		if got != c.want {
			t.Errorf("Items[%d].Author = %q, want %q", c.idx, got, c.want)
		}
	}
}

// --- Enclosure → Image ---

const rssEnclosure = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Enclosure Feed</title>
    <item>
      <guid>enc-1</guid>
      <title>With Enclosure</title>
      <enclosure url="https://example.com/image.jpg" type="image/jpeg" length="12345"/>
    </item>
  </channel>
</rss>`

func TestEnclosureImage(t *testing.T) {
	feed, err := Parse([]byte(rssEnclosure))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(feed.Items) == 0 {
		t.Fatal("no items")
	}
	if feed.Items[0].Image != "https://example.com/image.jpg" {
		t.Errorf("Image = %q, want %q", feed.Items[0].Image, "https://example.com/image.jpg")
	}
}

// --- 不正な入力 ---

func TestParseInvalidInput(t *testing.T) {
	cases := []string{
		"",
		"not xml or json at all",
		"<broken><xml>",
	}
	for _, input := range cases {
		_, err := Parse([]byte(input))
		if err == nil {
			t.Errorf("Parse(%q) should return error, got nil", strings.TrimSpace(input)[:min(len(input), 20)])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
