package parser

import "time"

type Feed struct {
	Title   string
	SiteURL string
	Items   []Item
}

type Item struct {
	GUID    string
	Title   string
	Link    string
	Date    time.Time
	Content string
	Author  string
	Image   string
}
