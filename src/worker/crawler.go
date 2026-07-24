package worker

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
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
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hayari/1.0 (+https://forge.harakara.site/littleisland/hayari)")
	req.Header.Set("Accept", "text/html, application/xhtml+xml, application/xml;q=0.9, */*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, pageURL)
	}

	return extractFeedLinks(io.LimitReader(resp.Body, 2<<20), pageURL)
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
			var rel, href, title, typ string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				case "title":
					title = attr.Val
				case "type":
					typ = attr.Val
				}
			}
			if isFeedRel(rel) || isFeedType(typ) {
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

func isFeedRel(rel string) bool {
	return strings.Contains(rel, "alternate")
}

func isFeedType(typ string) bool {
	feedTypes := []string{
		"application/rss+xml",
		"application/atom+xml",
		"application/json",
		"application/feed+json",
	}
	for _, ft := range feedTypes {
		if strings.EqualFold(typ, ft) {
			return true
		}
	}
	return false
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
	resp, err := crawlerClient.Get(siteURL)
	if err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
		resp.Body.Close()
		if iconURL := findFaviconURL(bytes.NewReader(body), base); iconURL != "" {
			if dataURL, err := fetchAsDataURL(iconURL); err == nil {
				return dataURL, nil
			}
		}
	}

	// Fallback: /favicon.ico at the root
	faviconURL := base.Scheme + "://" + base.Host + "/favicon.ico"
	return fetchAsDataURL(faviconURL)
}

// findFaviconURL parses HTML and returns the first icon <link> href, resolved to absolute.
func findFaviconURL(r io.Reader, base *url.URL) string {
	z := html.NewTokenizer(r)
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
			return resolveURL(base, href)
		}
	}
	return ""
}

// fetchAsDataURL downloads an image URL and returns it as a base64 data URL.
func fetchAsDataURL(iconURL string) (string, error) {
	resp, err := crawlerClient.Get(iconURL)
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

	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
