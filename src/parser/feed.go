package parser

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

func Parse(data []byte) (*Feed, error) {
	// A parser per call: gofeed.Parser lazily initializes internal state,
	// which is not safe under concurrent Parse calls from the worker pool.
	gf, err := gofeed.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	feed := &Feed{
		Title:   gf.Title,
		SiteURL: gf.Link,
	}

	for _, gi := range gf.Items {
		item := Item{
			GUID:    itemGUID(gi),
			Title:   gi.Title,
			Link:    gi.Link,
			Date:    itemDate(gi),
			Content: itemContent(gi),
			Author:  itemAuthor(gi),
		}
		if len(gi.Enclosures) > 0 {
			item.Image = gi.Enclosures[0].URL
		}
		feed.Items = append(feed.Items, item)
	}

	return feed, nil
}

func itemGUID(gi *gofeed.Item) string {
	if gi.GUID != "" {
		return gi.GUID
	}
	// A stable article URL is a better fallback than link+title: publishers
	// commonly revise titles after an item has first appeared in a feed. Keep
	// the title when no URL is available so distinct linkless entries remain
	// distinguishable.
	key := gi.Link
	if key == "" {
		key = "\x00" + gi.Title
	}
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h[:16])
}

func itemDate(gi *gofeed.Item) time.Time {
	if gi.PublishedParsed != nil {
		return *gi.PublishedParsed
	}
	if gi.UpdatedParsed != nil {
		return *gi.UpdatedParsed
	}
	return time.Now()
}

func itemContent(gi *gofeed.Item) string {
	if gi.Content != "" {
		return gi.Content
	}
	return gi.Description
}

func itemAuthor(gi *gofeed.Item) string {
	if gi.Author != nil {
		parts := []string{}
		if gi.Author.Name != "" {
			parts = append(parts, gi.Author.Name)
		}
		if gi.Author.Email != "" {
			parts = append(parts, gi.Author.Email)
		}
		return strings.Join(parts, " ")
	}
	return ""
}
