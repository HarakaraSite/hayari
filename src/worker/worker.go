package worker

import (
	"log"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"forge.harakara.site/littleisland/hayari/src/content"
	"forge.harakara.site/littleisland/hayari/src/parser"
	"forge.harakara.site/littleisland/hayari/src/storage"
)

const (
	defaultInterval = 30 * time.Minute
	cleanupInterval = 24 * time.Hour
	concurrentFeeds = 5
)

type Worker struct {
	db      *storage.Storage
	running atomic.Bool
	stop    chan struct{}
	mu      sync.Mutex
}

func New(db *storage.Storage) *Worker {
	return &Worker{
		db:   db,
		stop: make(chan struct{}),
	}
}

func (w *Worker) Running() bool {
	return w.running.Load()
}

func (w *Worker) Start() {
	go w.loop()
}

func (w *Worker) Stop() {
	close(w.stop)
}

// refreshInterval reads refresh_rate from settings (minutes).
// Returns 0 if auto-refresh is disabled or not set.
func (w *Worker) refreshInterval() time.Duration {
	val, err := w.db.GetSetting("refresh_rate")
	if err != nil || val == "" {
		return defaultInterval
	}
	mins, err := strconv.Atoi(val)
	if err != nil || mins <= 0 {
		return 0 // disabled
	}
	return time.Duration(mins) * time.Minute
}

func (w *Worker) loop() {
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()

	// Re-read the refresh_rate setting every minute so changes (including
	// disabling/re-enabling) take effect without a restart. The elapsed-time
	// check keeps the schedule stable across re-reads.
	checkTicker := time.NewTicker(time.Minute)
	defer checkTicker.Stop()

	w.RefreshAll()
	lastRefresh := time.Now()

	for {
		select {
		case <-checkTicker.C:
			if interval := w.refreshInterval(); interval > 0 && time.Since(lastRefresh) >= interval {
				w.RefreshAll()
				lastRefresh = time.Now()
			}
		case <-cleanupTicker.C:
			if err := w.db.DeleteOldItems(); err != nil {
				log.Printf("worker: cleanup: %v", err)
			}
		case <-w.stop:
			return
		}
	}
}

func (w *Worker) RefreshAll() {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	defer w.running.Store(false)

	feeds, err := w.db.ListFeeds()
	if err != nil {
		log.Printf("worker: list feeds: %v", err)
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrentFeeds)

	for _, feed := range feeds {
		wg.Add(1)
		go func(f storage.Feed) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.refreshFeed(f)
		}(feed)
	}
	wg.Wait()
}

func (w *Worker) RefreshFeed(id int64) {
	feed, err := w.db.GetFeed(id)
	if err != nil || feed == nil {
		return
	}
	w.refreshFeed(*feed)
}

func (w *Worker) refreshFeed(feed storage.Feed) {
	result, err := Fetch(feed.FeedURL, feed.LastModified, feed.ETag)
	if err != nil {
		log.Printf("worker: fetch %s: %v", feed.FeedURL, err)
		w.db.UpdateFeedError(feed.ID, err.Error())
		return
	}
	if result.NotModified {
		return
	}

	parsed, err := parser.Parse(result.Data)
	if err != nil {
		log.Printf("worker: parse %s: %v", feed.FeedURL, err)
		w.db.UpdateFeedError(feed.ID, err.Error())
		return
	}

	// Fill in missing metadata (UpdateFeedMeta never overwrites non-empty fields)
	needsMeta := (feed.Title == "" && parsed.Title != "") ||
		(feed.SiteURL == "" && parsed.SiteURL != "")
	if needsMeta {
		w.db.UpdateFeedMeta(feed.ID, parsed.Title, parsed.SiteURL)
	}

	// Mark refresh successful (clears last_error)
	var lm, etag *string
	if result.LastModified != "" {
		lm = &result.LastModified
	}
	if result.ETag != "" {
		etag = &result.ETag
	}
	w.db.UpdateFeedRefresh(feed.ID, lm, etag)

	// Fetch favicon once when not yet stored
	siteURL := parsed.SiteURL
	if siteURL == "" {
		siteURL = feed.SiteURL
	}
	if feed.Icon == nil && siteURL != "" {
		go w.fetchAndStoreFavicon(feed.ID, siteURL)
	}

	// Apply filters and collect items
	filters, _ := w.db.ListFiltersForFeed(feed.ID)
	var items []storage.Item
	for _, pi := range parsed.Items {
		item := storage.Item{
			FeedID:  feed.ID,
			GUID:    pi.GUID,
			Title:   pi.Title,
			Link:    content.Link(pi.Link),
			Date:    pi.Date,
			Content: content.HTML(pi.Content),
			Author:  pi.Author,
		}
		if pi.Image != "" {
			item.Image = &pi.Image
		}
		item.Status, item.Hidden = applyFilters(item, filters)
		items = append(items, item)
	}

	if len(items) > 0 {
		if err := w.db.CreateItems(items); err != nil {
			log.Printf("worker: insert items for %s: %v", feed.FeedURL, err)
		}
	}
}

func (w *Worker) fetchAndStoreFavicon(feedID int64, siteURL string) {
	dataURL, err := FetchFavicon(siteURL)
	if err != nil {
		log.Printf("worker: favicon %s: %v", siteURL, err)
		return
	}
	// UpdateFeedIcon only writes when icon IS NULL, so concurrent fetches
	// cannot clobber each other or any other feed fields.
	w.db.UpdateFeedIcon(feedID, &dataURL)
}

// applyFilters returns the status and hidden state the item should have based
// on the existing generic filter rules.
func applyFilters(item storage.Item, filters []storage.Filter) (string, bool) {
	for _, f := range filters {
		var field string
		switch f.Field {
		case "title":
			field = item.Title
		case "content":
			field = item.Content
		case "author":
			field = item.Author
		default:
			continue
		}

		matched, err := regexp.MatchString(f.Rule, field)
		if err != nil || !matched {
			continue
		}

		switch f.Action {
		case "mark_read":
			return "read", false
		case "hide":
			return "unread", true
		}
	}
	return "unread", false
}
