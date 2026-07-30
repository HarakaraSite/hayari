package worker

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"forge.harakara.site/littleisland/hayari/src/safehttp"
	"golang.org/x/net/html"
)

var crawlerClient = safehttp.NewClient(15 * time.Second)

type FoundFeed struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// FindFeeds attempts to find feed URLs from a web page URL.
func FindFeeds(pageURL string) ([]FoundFeed, error) {
	return findFeeds(crawlerClient, pageURL)
}

func findFeeds(client *http.Client, pageURL string) ([]FoundFeed, error) {
	req, err := newGetRequest(pageURL, "text/html, application/xhtml+xml, application/xml;q=0.9, */*;q=0.8")
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, pageURL)
	}

	feeds, err := extractFeedLinks(io.LimitReader(resp.Body, 2<<20), pageURL)
	if err != nil {
		return nil, err
	}
	if len(feeds) == 0 {
		// A directly entered YouTube channel RSS document does not advertise
		// itself with a feed MIME type, but its URL is enough to offer variants.
		return youtubeFeedVariants(FoundFeed{URL: pageURL}), nil
	}
	return expandYouTubeFeedVariants(feeds), nil
}

func extractFeedLinks(r io.Reader, baseURL string) ([]FoundFeed, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	var feeds []FoundFeed
	seen := map[string]bool{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var href, title, typ string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "href":
					href = attr.Val
				case "title":
					title = attr.Val
				case "type":
					typ = attr.Val
				}
			}
			// rel="alternate" describes any alternative representation, such as
			// a mobile page or application deep link. Only a feed-specific MIME
			// type identifies this link as a feed.
			if isFeedType(typ) {
				if href != "" {
					absHref := resolveURL(base, href)
					if !seen[absHref] {
						seen[absHref] = true
						feeds = append(feeds, FoundFeed{URL: absHref, Title: title})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return feeds, nil
}

// expandYouTubeFeedVariants adds selectable YouTube playlist feeds for each
// discovered channel RSS feed. It never replaces the channel's All feed.
func expandYouTubeFeedVariants(feeds []FoundFeed) []FoundFeed {
	result := make([]FoundFeed, 0, len(feeds)+3)
	for _, feed := range feeds {
		if variants := youtubeFeedVariants(feed); len(variants) > 0 {
			result = append(result, variants...)
			continue
		}
		result = append(result, feed)
	}
	return result
}

func youtubeFeedVariants(feed FoundFeed) []FoundFeed {
	u, err := url.Parse(feed.URL)
	if err != nil || (u.Hostname() != "youtube.com" && u.Hostname() != "www.youtube.com") || u.Path != "/feeds/videos.xml" {
		return nil
	}

	channelID := u.Query().Get("channel_id")
	if !strings.HasPrefix(channelID, "UC") || len(channelID) == len("UC") {
		return nil
	}

	playlistURL := func(prefix string) string {
		playlist := *u
		playlist.RawQuery = url.Values{"playlist_id": {prefix + strings.TrimPrefix(channelID, "UC")}}.Encode()
		return playlist.String()
	}

	return []FoundFeed{
		{URL: feed.URL, Title: "All"},
		{URL: playlistURL("UULF"), Title: "Videos"},
		{URL: playlistURL("UULV"), Title: "Live Streams"},
		{URL: playlistURL("UUSH"), Title: "Shorts"},
	}
}

func isFeedType(typ string) bool {
	mediaType, _, err := mime.ParseMediaType(typ)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "application/rss+xml", "application/atom+xml", "application/feed+json":
		return true
	default:
		return false
	}
}

func resolveURL(base *url.URL, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

// --- Favicon ---

var validIconMIME = map[string]string{
	"image/png":                "image/png",
	"image/jpeg":               "image/jpeg",
	"image/gif":                "image/gif",
	"image/webp":               "image/webp",
	"image/x-icon":             "image/x-icon",
	"image/vnd.microsoft.icon": "image/x-icon",
}

// FetchFavicon fetches the favicon for siteURL and returns a data URL string.
// It first looks for <link rel="icon"> in the HTML, then falls back to /favicon.ico.
func FetchFavicon(siteURL string) (string, error) {
	base, err := url.Parse(siteURL)
	if err != nil {
		return "", err
	}

	// Try to extract icon URL from the HTML
	req, err := newGetRequest(siteURL, "text/html, application/xhtml+xml, */*;q=0.8")
	if err != nil {
		return "", err
	}
	resp, err := crawlerClient.Do(req)
	if err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
		resp.Body.Close()
		for _, iconURL := range findFaviconURLs(bytes.NewReader(body), base) {
			if dataURL, err := fetchAsDataURL(iconURL); err == nil {
				return dataURL, nil
			}
		}
	}

	// Fallback: /favicon.ico at the root
	faviconURL := base.Scheme + "://" + base.Host + "/favicon.ico"
	return fetchAsDataURL(faviconURL)
}

// findFaviconURLs parses HTML and returns icon <link> hrefs, resolved to absolute.
// A page can provide multiple icon sizes; callers should try each because an advertised
// derivative can be missing even when the original icon is available.
func findFaviconURLs(r io.Reader, base *url.URL) []string {
	z := html.NewTokenizer(r)
	var iconURLs []string
	seen := make(map[string]struct{})
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.SelfClosingTagToken && tt != html.StartTagToken {
			continue
		}
		name, _ := z.TagName()
		if string(name) != "link" {
			continue
		}
		var rel, href string
		for {
			k, v, more := z.TagAttr()
			switch string(k) {
			case "rel":
				rel = strings.ToLower(string(v))
			case "href":
				href = string(v)
			}
			if !more {
				break
			}
		}
		if strings.Contains(rel, "icon") && href != "" {
			iconURL := resolveURL(base, href)
			if _, ok := seen[iconURL]; !ok {
				seen[iconURL] = struct{}{}
				iconURLs = append(iconURLs, iconURL)
			}
		}
	}
	return iconURLs
}

// fetchAsDataURL downloads an image URL and returns it as a base64 data URL.
func fetchAsDataURL(iconURL string) (string, error) {
	req, err := newGetRequest(iconURL, "image/avif, image/webp, image/png, image/jpeg, image/gif, image/x-icon, */*;q=0.8")
	if err != nil {
		return "", err
	}
	resp, err := crawlerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if i := strings.Index(ct, ";"); i != -1 {
		ct = strings.TrimSpace(ct[:i])
	}
	mimeType, ok := validIconMIME[ct]
	if !ok {
		return "", fmt.Errorf("unsupported icon type: %s", ct)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512KB limit
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty icon response")
	}

	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
