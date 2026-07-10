package worker

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nkanaev/yarr2/src/safehttp"
)

var httpClient = safehttp.NewClient(30 * time.Second)

type FetchResult struct {
	Data         []byte
	LastModified string
	ETag         string
	NotModified  bool
}

func Fetch(url string, lastModified, etag *string) (*FetchResult, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "yarr2/1.0 (+https://github.com/nkanaev/yarr2)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/json, text/xml, */*")

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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &FetchResult{
		Data:         data,
		LastModified: resp.Header.Get("Last-Modified"),
		ETag:         resp.Header.Get("ETag"),
	}, nil
}
