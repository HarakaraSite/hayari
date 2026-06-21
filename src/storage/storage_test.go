package storage

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *Storage {
	t.Helper()
	f, err := os.CreateTemp("", "yarr2-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := Open(f.Name())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Folder ---

func TestFolderCRUD(t *testing.T) {
	s := newTestDB(t)

	folder, err := s.CreateFolder("Tech")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if folder.Title != "Tech" {
		t.Errorf("Title = %q, want %q", folder.Title, "Tech")
	}
	if folder.IsExpanded {
		t.Error("IsExpanded should default to false")
	}

	// Update title
	if err := s.UpdateFolder(folder.ID, "Technology"); err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	got, _ := s.GetFolder(folder.ID)
	if got.Title != "Technology" {
		t.Errorf("after update Title = %q, want %q", got.Title, "Technology")
	}

	// Update is_expanded
	if err := s.UpdateFolderExpanded(folder.ID, true); err != nil {
		t.Fatalf("UpdateFolderExpanded: %v", err)
	}
	got, _ = s.GetFolder(folder.ID)
	if !got.IsExpanded {
		t.Error("IsExpanded should be true after update")
	}

	// List
	folders, err := s.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("ListFolders count = %d, want 1", len(folders))
	}

	// Delete
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	folders, _ = s.ListFolders()
	if len(folders) != 0 {
		t.Error("folder should be deleted")
	}
}

func TestGetOrCreateFolder(t *testing.T) {
	s := newTestDB(t)

	f1, err := s.GetOrCreateFolder("News")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := s.GetOrCreateFolder("News")
	if err != nil {
		t.Fatal(err)
	}
	if f1.ID != f2.ID {
		t.Error("GetOrCreateFolder should return the same folder on second call")
	}
}

// --- Feed ---

func TestFeedCRUD(t *testing.T) {
	s := newTestDB(t)

	feed, err := s.CreateFeed("https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}
	if feed.FeedURL != "https://example.com/feed.xml" {
		t.Errorf("FeedURL = %q", feed.FeedURL)
	}
	if feed.LastError != nil {
		t.Error("LastError should be nil on creation")
	}

	// Duplicate URL returns existing
	feed2, err := s.CreateFeed("https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if feed.ID != feed2.ID {
		t.Error("duplicate feed URL should return same feed")
	}

	// UpdateFeedMeta
	if err := s.UpdateFeedMeta(feed.ID, "Example", "https://example.com"); err != nil {
		t.Fatalf("UpdateFeedMeta: %v", err)
	}
	got, _ := s.GetFeed(feed.ID)
	if got.Title != "Example" {
		t.Errorf("Title = %q, want %q", got.Title, "Example")
	}

	// UpdateFeedError
	if err := s.UpdateFeedError(feed.ID, "timeout"); err != nil {
		t.Fatalf("UpdateFeedError: %v", err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.LastError == nil || *got.LastError != "timeout" {
		t.Errorf("LastError = %v, want 'timeout'", got.LastError)
	}

	// UpdateFeedRefresh clears error
	if err := s.UpdateFeedRefresh(feed.ID, nil, nil); err != nil {
		t.Fatalf("UpdateFeedRefresh: %v", err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.LastError != nil {
		t.Errorf("LastError should be cleared after refresh, got %v", *got.LastError)
	}
}

func TestListFeedErrors(t *testing.T) {
	s := newTestDB(t)

	f1, _ := s.CreateFeed("https://a.com/feed", nil)
	f2, _ := s.CreateFeed("https://b.com/feed", nil)
	s.CreateFeed("https://c.com/feed", nil)

	s.UpdateFeedError(f1.ID, "connection refused")
	s.UpdateFeedError(f2.ID, "HTTP 404")

	errs, err := s.ListFeedErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 2 {
		t.Fatalf("ListFeedErrors count = %d, want 2", len(errs))
	}
	if errs[f1.ID] != "connection refused" {
		t.Errorf("f1 error = %q", errs[f1.ID])
	}
}

// --- Item ---

func makeItem(feedID int64, guid, title, content string) Item {
	return Item{
		FeedID:  feedID,
		GUID:    guid,
		Title:   title,
		Link:    "https://example.com/" + guid,
		Date:    time.Now(),
		Content: content,
		Author:  "test",
		Status:  "unread",
	}
}

func TestItemCRUD(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)

	items := []Item{
		makeItem(feed.ID, "guid-1", "First", "<p>Hello world</p>"),
		makeItem(feed.ID, "guid-2", "Second", "<p>Go is great</p>"),
	}
	if err := s.CreateItems(items); err != nil {
		t.Fatalf("CreateItems: %v", err)
	}

	// List all
	got, err := s.ListItems(ItemFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListItems count = %d, want 2", len(got))
	}

	// Count
	count, err := s.CountItems(ItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("CountItems = %d, want 2", count)
	}

	// Duplicate insert is a no-op
	if err := s.CreateItems(items); err != nil {
		t.Fatalf("duplicate CreateItems should not error: %v", err)
	}
	count, _ = s.CountItems(ItemFilter{})
	if count != 2 {
		t.Errorf("after duplicate insert count = %d, want 2", count)
	}

	// UpdateItemStatus
	id := got[0].ID
	if err := s.UpdateItemStatus(id, "read"); err != nil {
		t.Fatal(err)
	}
	item, _ := s.GetItem(id)
	if item.Status != "read" {
		t.Errorf("Status = %q, want %q", item.Status, "read")
	}

	// Filter by status
	unread, _ := s.ListItems(ItemFilter{Status: "unread"}, 10, 0)
	if len(unread) != 1 {
		t.Errorf("unread count = %d, want 1", len(unread))
	}
}

func TestItemFTSSearch(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)
	items := []Item{
		makeItem(feed.ID, "g1", "Golang Tips", "<p>Learn Go programming</p>"),
		makeItem(feed.ID, "g2", "Python Guide", "<p>Python is also great</p>"),
		makeItem(feed.ID, "g3", "Rust Basics", "<p>Memory safety in Rust</p>"),
	}
	s.CreateItems(items)

	// Search by title word
	results, err := s.ListItems(ItemFilter{Search: "Golang"}, 10, 0)
	if err != nil {
		t.Fatalf("ListItems with search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search 'Golang' count = %d, want 1", len(results))
	}

	// Search by content word
	results, err = s.ListItems(ItemFilter{Search: "programming"}, 10, 0)
	if err != nil {
		t.Fatalf("ListItems with search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search 'programming' count = %d, want 1", len(results))
	}

	// No match
	results, err = s.ListItems(ItemFilter{Search: "javascript"}, 10, 0)
	if err != nil {
		t.Fatalf("ListItems with search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("search 'javascript' count = %d, want 0", len(results))
	}
}

func TestMarkAllRead(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)
	s.CreateItems([]Item{
		makeItem(feed.ID, "g1", "A", ""),
		makeItem(feed.ID, "g2", "B", ""),
		makeItem(feed.ID, "g3", "C", ""),
	})

	if err := s.MarkAllRead(ItemFilter{FeedID: &feed.ID}); err != nil {
		t.Fatal(err)
	}
	unread, _ := s.ListItems(ItemFilter{Status: "unread"}, 10, 0)
	if len(unread) != 0 {
		t.Errorf("after MarkAllRead unread = %d, want 0", len(unread))
	}
}

func TestDeleteOldItems(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)

	// Insert 60 recent items
	var recentItems []Item
	for i := 0; i < 60; i++ {
		item := makeItem(feed.ID, fmt.Sprintf("recent-%d", i), fmt.Sprintf("Recent %d", i), "")
		recentItems = append(recentItems, item)
	}
	s.CreateItems(recentItems)

	// Insert 5 old items (manually set date via raw SQL after CreateItems won't work,
	// so use a helper approach: create with old date directly)
	tx, _ := s.db.Begin()
	oldDate := time.Now().AddDate(0, 0, -100).Format("2006-01-02 15:04:05")
	for i := 0; i < 5; i++ {
		tx.Exec(`INSERT OR IGNORE INTO items (feed_id, guid, title, link, date, content, author, status)
			VALUES (?, ?, ?, ?, ?, '', '', 'unread')`,
			feed.ID, fmt.Sprintf("old-%d", i), fmt.Sprintf("Old %d", i), "https://x.com", oldDate)
	}
	tx.Commit()

	total, _ := s.CountItems(ItemFilter{})
	if total != 65 {
		t.Fatalf("before delete total = %d, want 65", total)
	}

	if err := s.DeleteOldItems(); err != nil {
		t.Fatalf("DeleteOldItems: %v", err)
	}

	// Old items beyond the top-50 protection should be removed.
	// We have 60 recent + 5 old = 65. Top 50 are the most recent 50.
	// The 10 oldest recent items + 5 old items are candidates.
	// Old items (>90 days) = 5 should be deleted (they're not in top 50 since 60 recent exist).
	after, _ := s.CountItems(ItemFilter{})
	if after != 60 {
		t.Errorf("after delete total = %d, want 60", after)
	}
}

func TestDeleteOldItemsKeepsStarred(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)

	// Insert 1 old starred item using the new schema (starred column)
	tx, _ := s.db.Begin()
	oldDate := time.Now().AddDate(0, 0, -100).Format("2006-01-02 15:04:05")
	tx.Exec(`INSERT INTO items (feed_id, guid, title, link, date, content, author, status, starred)
		VALUES (?, 'starred-old', 'Starred Old', 'https://x.com', ?, '', '', 'read', 1)`,
		feed.ID, oldDate)
	tx.Commit()

	if err := s.DeleteOldItems(); err != nil {
		t.Fatal(err)
	}

	items, _ := s.ListItems(ItemFilter{}, 10, 0)
	if len(items) != 1 {
		t.Errorf("starred item should not be deleted, got %d items", len(items))
	}
	if !items[0].Starred {
		t.Error("item.Starred should be true")
	}
}

func TestSetStarred(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)
	s.CreateItems([]Item{makeItem(feed.ID, "g1", "A", "")})

	items, _ := s.ListItems(ItemFilter{}, 10, 0)
	id := items[0].ID

	if err := s.SetStarred(id, true); err != nil {
		t.Fatal(err)
	}
	item, _ := s.GetItem(id)
	if !item.Starred {
		t.Error("item should be starred")
	}
	// Status should be unchanged
	if item.Status != "unread" {
		t.Errorf("status = %q, want %q", item.Status, "unread")
	}

	// Filter by starred
	starred, _ := s.ListItems(ItemFilter{Starred: boolPtr(true)}, 10, 0)
	if len(starred) != 1 {
		t.Errorf("starred filter count = %d, want 1", len(starred))
	}

	if err := s.SetStarred(id, false); err != nil {
		t.Fatal(err)
	}
	item, _ = s.GetItem(id)
	if item.Starred {
		t.Error("item should not be starred after unstar")
	}
}

func boolPtr(v bool) *bool { return &v }

// --- UpdateFeedMeta / UpdateFeedFolder / UpdateFeedIcon semantics ---

func TestUpdateFeedMetaDoesNotOverwrite(t *testing.T) {
	s := newTestDB(t)
	feed, _ := s.CreateFeed("https://example.com/feed.xml", nil)

	// Fill empty fields
	if err := s.UpdateFeedMeta(feed.ID, "Parsed Title", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetFeed(feed.ID)
	if got.Title != "Parsed Title" || got.SiteURL != "https://example.com" {
		t.Fatalf("empty fields not filled: %+v", got)
	}

	// Second call must not overwrite existing values
	if err := s.UpdateFeedMeta(feed.ID, "Other Title", "https://other.example.com"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.Title != "Parsed Title" {
		t.Fatalf("non-empty title overwritten: %q", got.Title)
	}
	if got.SiteURL != "https://example.com" {
		t.Fatalf("non-empty site_url overwritten: %q", got.SiteURL)
	}

	// Empty parsed values must not clear existing ones
	if err := s.UpdateFeedMeta(feed.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.Title == "" || got.SiteURL == "" {
		t.Fatalf("fields cleared by empty meta: %+v", got)
	}
}

func TestUpdateFeedFolderClears(t *testing.T) {
	s := newTestDB(t)
	folder, _ := s.CreateFolder("News")
	feed, _ := s.CreateFeed("https://example.com/feed.xml", &folder.ID)

	got, _ := s.GetFeed(feed.ID)
	if got.FolderID == nil || *got.FolderID != folder.ID {
		t.Fatalf("setup: feed not in folder: %+v", got)
	}

	if err := s.UpdateFeedFolder(feed.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.FolderID != nil {
		t.Fatalf("folder not cleared: %+v", got.FolderID)
	}

	if err := s.UpdateFeedFolder(feed.ID, &folder.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeed(feed.ID)
	if got.FolderID == nil || *got.FolderID != folder.ID {
		t.Fatal("folder not re-assigned")
	}
}

func TestUpdateFeedIconOnlyWhenNull(t *testing.T) {
	s := newTestDB(t)
	feed, _ := s.CreateFeed("https://example.com/feed.xml", nil)

	first := "data:image/png;base64,AAAA"
	if err := s.UpdateFeedIcon(feed.ID, &first); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetFeed(feed.ID)
	if got.Icon == nil || *got.Icon != first {
		t.Fatal("icon not stored")
	}

	second := "data:image/png;base64,BBBB"
	if err := s.UpdateFeedIcon(feed.ID, &second); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeed(feed.ID)
	if *got.Icon != first {
		t.Fatalf("existing icon overwritten: %q", *got.Icon)
	}
}

func TestMarkAllReadEmptyFilter(t *testing.T) {
	s := newTestDB(t)

	feed, _ := s.CreateFeed("https://example.com/feed", nil)
	s.CreateItems([]Item{
		makeItem(feed.ID, "g1", "A", ""),
		makeItem(feed.ID, "g2", "B", ""),
	})

	if err := s.MarkAllRead(ItemFilter{}); err != nil {
		t.Fatal(err)
	}
	unread, _ := s.ListItems(ItemFilter{Status: "unread"}, 10, 0)
	if len(unread) != 0 {
		t.Errorf("after MarkAllRead unread = %d, want 0", len(unread))
	}
}
