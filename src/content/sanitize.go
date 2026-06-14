// Package content provides sanitization for untrusted HTML from RSS feeds.
package content

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Policies are built once at startup; Sanitize is safe for concurrent use.
var (
	policy     = buildPolicy()
	pagePolicy = buildPagePolicy()
)

func buildPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Allow common feed presentation tags not in default UGC policy
	p.AllowElements("figure", "figcaption", "picture", "details", "summary")
	p.AllowAttrs("srcset", "sizes").OnElements("img", "source")
	p.AllowAttrs("loading").OnElements("img")
	// Allow pre/code with language class (syntax highlighting)
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("code", "pre", "span")
	return p
}

// buildPagePolicy extends the feed policy for full-page fetches (/page,
// readability mode). The client extracts the main article via selectors like
// "main", "[role=main]", ".entry-content", so those tags/attributes must
// survive sanitization. class/role carry no script-execution risk.
func buildPagePolicy() *bluemonday.Policy {
	p := buildPolicy()
	p.AllowElements("main")
	// "main" is not in bluemonday's default allowed-without-attributes list,
	// so a bare <main> would still be dropped without this.
	p.AllowNoAttrs().OnElements("main")
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).Globally()
	p.AllowAttrs("role").Matching(bluemonday.SpaceSeparatedTokens).Globally()
	return p
}

// HTML sanitizes untrusted HTML from feed content.
// Removes all event handlers, dangerous tags (script/iframe/object/embed),
// and javascript:/data: URLs. Safe tags and attributes are preserved.
func HTML(s string) string {
	return policy.Sanitize(s)
}

// PageHTML sanitizes a full web page fetched for readability mode.
// Same safety guarantees as HTML, but preserves structural markers
// (main, class, role) that the client uses to locate the article body.
func PageHTML(s string) string {
	return pagePolicy.Sanitize(s)
}

// Link validates that a URL uses http or https scheme.
// Returns empty string for any other scheme (javascript:, data:, vbscript:, etc.).
func Link(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return s
	}
	return ""
}
