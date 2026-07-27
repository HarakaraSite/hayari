package worker

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"forge.harakara.site/littleisland/hayari/src/safehttp"
)

var httpClient = safehttp.NewClient(30 * time.Second)

const maxFeedSize = 5 << 20 // 5 MiB

const userAgentURL = "https://forge.harakara.site/littleisland/hayari"

var version = "dev"

// SetVersion configures the version used to identify Hayari to feed and favicon hosts.
// It must be called during startup before requests are made.
func SetVersion(value string) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		value = "dev"
	}
	version = value
}

// UserAgent returns the consistent identifier used for all external fetches.
func UserAgent() string {
	return fmt.Sprintf("Hayari/%s (+%s)", version, userAgentURL)
}

func newGetRequest(url, accept string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return req, nil
}

type FetchResult struct {
	Data         []byte
	LastModified string
	ETag         string
	NotModified  bool
}

func Fetch(url string, lastModified, etag *string) (*FetchResult, error) {
	req, err := newGetRequest(url, "application/rss+xml, application/atom+xml, application/json, text/xml, */*")
	if err != nil {
		return nil, err
	}

	if lastModified != nil && *lastModified != "" {
		req.Header.Set("If-Modified-Since", *lastModified)
	}
	if etag != nil && *etag != "" {
		req.Header.Set("If-None-Match", *etag)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &FetchResult{NotModified: true}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	if resp.ContentLength > maxFeedSize {
		return nil, fmt.Errorf("feed response exceeds %d byte limit: %s", maxFeedSize, url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFeedSize {
		return nil, fmt.Errorf("feed response exceeds %d byte limit: %s", maxFeedSize, url)
	}

	return &FetchResult{
		Data:         data,
		LastModified: resp.Header.Get("Last-Modified"),
		ETag:         resp.Header.Get("ETag"),
	}, nil
}
