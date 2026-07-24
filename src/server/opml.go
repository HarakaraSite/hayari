package server

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

// --- OPML XML structures ---

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title string `xml:"title"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	XMLUrl   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLUrl  string        `xml:"htmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline"`
}

// --- Import ---

func (s *Server) handleOPMLImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4MB limit
	if err != nil {
		httpError(w, err, 400)
		return
	}

	var doc opmlDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		httpError(w, err, 400)
		return
	}

	imported, err := s.importOutlines(doc.Body.Outlines, nil)
	if err != nil {
		httpError(w, err, 500)
		return
	}

	writeJSON(w, map[string]int{"imported": imported})
}

// importOutlines recursively creates folders and feeds from OPML outlines.
func (s *Server) importOutlines(outlines []opmlOutline, folderID *int64) (int, error) {
	count := 0
	for _, o := range outlines {
		if o.XMLUrl != "" {
			// Feed outline
			feed, err := s.db.CreateFeed(o.XMLUrl, folderID)
			if err != nil {
				return count, err
			}
			// Set title if present
			if o.Title != "" || o.Text != "" {
				name := o.Title
				if name == "" {
					name = o.Text
				}
				s.db.UpdateFeed(feed.ID, &name, folderID)
			}
			go s.worker.RefreshFeed(feed.ID)
			count++
		} else if len(o.Outlines) > 0 {
			// Folder outline
			name := o.Title
			if name == "" {
				name = o.Text
			}
			folder, err := s.db.GetOrCreateFolder(name)
			if err != nil {
				return count, err
			}
			n, err := s.importOutlines(o.Outlines, &folder.ID)
			if err != nil {
				return count + n, err
			}
			count += n
		}
	}
	return count, nil
}

// --- Export ---

func (s *Server) handleOPMLExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	feeds, err := s.db.ListFeeds()
	if err != nil {
		httpError(w, err, 500)
		return
	}
	folders, err := s.db.ListFolders()
	if err != nil {
		httpError(w, err, 500)
		return
	}

	// Group feeds by folder
	folderFeeds := make(map[int64][]storage.Feed)
	var unfoldered []storage.Feed
	for _, f := range feeds {
		if f.FolderID != nil {
			folderFeeds[*f.FolderID] = append(folderFeeds[*f.FolderID], f)
		} else {
			unfoldered = append(unfoldered, f)
		}
	}

	var outlines []opmlOutline

	// Folders first
	for _, folder := range folders {
		folderOutline := opmlOutline{
			Text:  folder.Title,
			Title: folder.Title,
		}
		for _, f := range folderFeeds[folder.ID] {
			folderOutline.Outlines = append(folderOutline.Outlines, feedToOutline(f))
		}
		outlines = append(outlines, folderOutline)
	}

	// Unfoldered feeds
	for _, f := range unfoldered {
		outlines = append(outlines, feedToOutline(f))
	}

	doc := opmlDoc{
		Version: "1.1",
		Head:    opmlHead{Title: "hayari subscriptions"},
		Body:    opmlBody{Outlines: outlines},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		httpError(w, err, 500)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="hayari.opml"`)
	fmt.Fprintf(w, "%s\n%s\n", xml.Header, out)
}

func feedToOutline(f storage.Feed) opmlOutline {
	return opmlOutline{
		Text:    f.Title,
		Title:   f.Title,
		Type:    "rss",
		XMLUrl:  f.FeedURL,
		HTMLUrl: f.SiteURL,
	}
}
