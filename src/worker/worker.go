package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"forge.harakara.site/littleisland/hayari/src/content"
	"forge.harakara.site/littleisland/hayari/src/parser"
	"forge.harakara.site/littleisland/hayari/src/storage"
)

const (
	defaultInterval       = 30 * time.Minute
	defaultItemMaxAgeDays = 30
	cleanupInterval       = 24 * time.Hour
	concurrentFeeds       = 5
)

type Worker struct {
	db      *storage.Storage
	running atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc

	refreshMu sync.Mutex
	refreshes map[int64]chan struct{}
	queue     []feedRefresh
	urgent    int // Number of Web subscription requests at the front of queue.
	workers   int
}

type feedRefresh struct {
	id   int64
	done chan struct{}
}

// ErrStopped indicates that the worker shut down while a caller was waiting.
var ErrStopped = errors.New("feed refresh worker stopped")

func New(db *storage.Storage) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		db:        db,
		ctx:       ctx,
		cancel:    cancel,
		refreshes: make(map[int64]chan struct{}),
	}
}

func (w *Worker) Running() bool {
	return w.running.Load()
}

func (w *Worker) Start() {
	go w.loop()
}

func (w *Worker) Stop() {
	w.refreshMu.Lock()
	defer w.refreshMu.Unlock()
	w.cancel()
	for _, job := range w.queue {
		delete(w.refreshes, job.id)
	}
	w.queue = nil
	w.urgent = 0
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

// itemMaxAgeDays reads item_max_age_days from settings. Zero disables the
// age limit; missing or invalid values retain the safe 30-day default.
func (w *Worker) itemMaxAgeDays() int {
	val, err := w.db.GetSetting("item_max_age_days")
	if err != nil || val == "" {
		return defaultItemMaxAgeDays
	}
	days, err := strconv.Atoi(val)
	if err != nil || days < 0 {
		return defaultItemMaxAgeDays
	}
	return days
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
			if err := w.db.Optimize(w.ctx); err != nil && w.ctx.Err() == nil {
				log.Printf("worker: optimize database query plans: %v", err)
			}
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *Worker) RefreshAll() {
	if w.running.CompareAndSwap(false, true) {
		w.refreshAll()
	}
}

// RefreshAllAsync publishes the running state before returning to the caller.
func (w *Worker) RefreshAllAsync() {
	if w.running.CompareAndSwap(false, true) {
		go w.refreshAll()
	}
}

func (w *Worker) refreshAll() {
	defer w.running.Store(false)

	ids, err := w.db.ListFeedIDs()
	if err != nil {
		log.Printf("worker: list feeds: %v", err)
		return
	}

	pending := make([]<-chan struct{}, 0, len(ids))
	for _, id := range ids {
		pending = append(pending, w.enqueueRefresh(id, false))
	}
	for _, done := range pending {
		select {
		case <-done:
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *Worker) RefreshFeed(id int64) {
	_ = w.RefreshFeedContext(context.Background(), id)
}

// RefreshFeedContext prioritizes the initial refresh for a Web subscription.
// Canceling the wait leaves the shared refresh queued for other callers.
func (w *Worker) RefreshFeedContext(ctx context.Context, id int64) error {
	select {
	case <-w.enqueueRefresh(id, true):
		if w.ctx.Err() != nil {
			return ErrStopped
		}
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return ErrStopped
	}
}

// RefreshFeedAsync queues a subscription's initial refresh.
func (w *Worker) RefreshFeedAsync(id int64) {
	w.enqueueRefresh(id, false)
}

func (w *Worker) enqueueRefresh(id int64, urgent bool) <-chan struct{} {
	w.refreshMu.Lock()
	defer w.refreshMu.Unlock()
	select {
	case <-w.ctx.Done():
		return nil
	default:
	}
	if done := w.refreshes[id]; done != nil {
		if urgent {
			for i := w.urgent; i < len(w.queue); i++ {
				job := w.queue[i]
				if job.id != id {
					continue
				}
				copy(w.queue[w.urgent+1:i+1], w.queue[w.urgent:i])
				w.queue[w.urgent] = job
				w.urgent++
				break
			}
		}
		return done
	}
	done := make(chan struct{})
	w.refreshes[id] = done
	job := feedRefresh{id: id, done: done}
	if urgent {
		w.queue = slices.Insert(w.queue, w.urgent, job)
		w.urgent++
	} else {
		w.queue = append(w.queue, job)
	}
	if w.workers < concurrentFeeds {
		w.workers++
		go w.runRefreshQueue()
	}
	return done
}

func (w *Worker) runRefreshQueue() {
	for {
		w.refreshMu.Lock()
		if len(w.queue) == 0 {
			w.queue = nil
			w.workers--
			w.refreshMu.Unlock()
			return
		}
		job := w.queue[0]
		w.queue[0] = feedRefresh{}
		w.queue = w.queue[1:]
		if w.urgent > 0 {
			w.urgent--
		}
		w.refreshMu.Unlock()

		w.refreshFeed(job.id)

		w.refreshMu.Lock()
		delete(w.refreshes, job.id)
		close(job.done)
		w.refreshMu.Unlock()
	}
}

func (w *Worker) refreshFeed(id int64) {
	if w.ctx.Err() != nil {
		return
	}
	feed, err := w.db.GetFeed(id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("worker: get feed %d: %v", id, err)
		}
		return
	}
	if err := w.fetchAndStoreFeed(feed); err != nil && !errors.Is(err, sql.ErrNoRows) && w.ctx.Err() == nil {
		log.Printf("worker: refresh %s: %v", feed.FeedURL, err)
		if err := w.db.UpdateFeedError(id, err.Error()); err != nil {
			log.Printf("worker: record refresh error for feed %d: %v", id, err)
		}
	}
}

func (w *Worker) fetchAndStoreFeed(feed *storage.Feed) error {
	result, err := fetch(w.ctx, feed.FeedURL, feed.LastModified, feed.ETag)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if result.NotModified {
		return nil
	}

	parsed, err := parser.Parse(result.Data)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	items, err := w.prepareItems(feed.ID, parsed.Items)
	if err != nil {
		return err
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	var lm, etag *string
	if result.LastModified != "" {
		lm = &result.LastModified
	}
	if result.ETag != "" {
		etag = &result.ETag
	}
	if err := w.db.StoreFeedRefresh(feed.ID, parsed.Title, parsed.SiteURL, lm, etag, items); err != nil {
		return fmt.Errorf("store feed refresh: %w", err)
	}

	// Fetch favicon once when not yet stored. Some Atom feeds, including the
	// Go Blog, only expose a rel="self" feed URL and no site URL. In that case
	// the feed URL still gives FetchFavicon the correct origin for /favicon.ico.
	siteURL := faviconSourceURL(parsed.SiteURL, feed.SiteURL, feed.FeedURL)
	if feed.Icon == nil && siteURL != "" {
		go w.fetchAndStoreFavicon(feed.ID, siteURL)
	}
	return nil
}

func (w *Worker) prepareItems(feedID int64, parsed []parser.Item) ([]storage.Item, error) {
	if len(parsed) == 0 {
		return nil, nil
	}
	maxAgeDays := w.itemMaxAgeDays()
	oldestAllowed := time.Now().AddDate(0, 0, -maxAgeDays)
	items := make([]storage.Item, 0, len(parsed))
	for _, pi := range parsed {
		if maxAgeDays > 0 && pi.Date.Before(oldestAllowed) {
			continue
		}
		item := storage.Item{
			FeedID:  feedID,
			GUID:    pi.GUID,
			Title:   pi.Title,
			Link:    content.Link(pi.Link),
			Date:    pi.Date,
			Content: pi.Content,
			Author:  pi.Author,
		}
		if pi.Image != "" {
			item.Image = &pi.Image
		}
		items = append(items, item)
	}

	items, err := w.db.NewFeedItems(feedID, items)
	if err != nil {
		return nil, fmt.Errorf("find new articles: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	filters, err := w.db.ListFiltersForFeed(feedID)
	if err != nil {
		return nil, fmt.Errorf("load article filters: %w", err)
	}
	compiled := compileFilters(filters)
	for i := range items {
		item := &items[i]
		item.Content = content.HTML(item.Content)
		item.Status, item.Hidden = applyFilters(*item, compiled)
	}
	return items, nil
}

func faviconSourceURL(parsedSiteURL, storedSiteURL, feedURL string) string {
	if parsedSiteURL != "" {
		return parsedSiteURL
	}
	if storedSiteURL != "" {
		return storedSiteURL
	}
	return feedURL
}

func (w *Worker) fetchAndStoreFavicon(feedID int64, siteURL string) {
	dataURL, err := FetchFavicon(siteURL)
	if err != nil {
		log.Printf("worker: favicon %s: %v", siteURL, err)
		return
	}
	if err := w.db.UpdateFeedIcon(feedID, &dataURL); err != nil {
		log.Printf("worker: store favicon for feed %d: %v", feedID, err)
	}
}

type compiledFilter struct {
	pattern *regexp.Regexp
	field   string
	action  string
}

func compileFilters(filters []storage.Filter) []compiledFilter {
	compiled := make([]compiledFilter, 0, len(filters))
	for _, filter := range filters {
		if pattern, err := regexp.Compile(filter.Rule); err == nil {
			compiled = append(compiled, compiledFilter{pattern: pattern, field: filter.Field, action: filter.Action})
		}
	}
	return compiled
}

// applyFilters returns the status and hidden state the item should have based
// on the existing generic filter rules.
func applyFilters(item storage.Item, filters []compiledFilter) (string, bool) {
	for _, f := range filters {
		var field string
		switch f.field {
		case "title":
			field = item.Title
		case "content":
			field = item.Content
		case "author":
			field = item.Author
		default:
			continue
		}

		if !f.pattern.MatchString(field) {
			continue
		}

		switch f.action {
		case "mark_read":
			return "read", false
		case "hide":
			return "unread", true
		}
	}
	return "unread", false
}
