package content

import (
	"strings"
	"testing"
)

func TestHTMLStripsScriptTag(t *testing.T) {
	out := HTML(`<p>hello</p><script>alert(1)</script>`)
	if strings.Contains(out, "script") {
		t.Fatalf("script tag not removed: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("safe content removed: %q", out)
	}
}

func TestHTMLStripsEventHandler(t *testing.T) {
	out := HTML(`<img src="x" onerror="alert(1)">`)
	if strings.Contains(out, "onerror") {
		t.Fatalf("onerror attribute not removed: %q", out)
	}
}

func TestHTMLStripsJavascriptHref(t *testing.T) {
	out := HTML(`<a href="javascript:alert(1)">click</a>`)
	if strings.Contains(out, "javascript:") {
		t.Fatalf("javascript: href not removed: %q", out)
	}
}

func TestHTMLPreservesSafeContent(t *testing.T) {
	in := `<p>Hello <strong>world</strong></p><a href="https://example.com">link</a>`
	out := HTML(in)
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "strong") || !strings.Contains(out, "example.com") {
		t.Fatalf("safe content stripped unexpectedly: %q", out)
	}
}

func TestLinkAllowsHTTP(t *testing.T) {
	if Link("http://example.com") != "http://example.com" {
		t.Fatal("http URL rejected")
	}
	if Link("https://example.com/path?q=1") != "https://example.com/path?q=1" {
		t.Fatal("https URL rejected")
	}
}

func TestLinkBlocksJavascript(t *testing.T) {
	if Link("javascript:alert(1)") != "" {
		t.Fatal("javascript: URL not blocked")
	}
	if Link("JAVASCRIPT:alert(1)") != "" {
		t.Fatal("uppercase JAVASCRIPT: URL not blocked")
	}
	if Link("data:text/html,<h1>x</h1>") != "" {
		t.Fatal("data: URL not blocked")
	}
	if Link("vbscript:msgbox(1)") != "" {
		t.Fatal("vbscript: URL not blocked")
	}
}

func TestLinkEmptyString(t *testing.T) {
	if Link("") != "" {
		t.Fatal("empty string should return empty")
	}
}

func TestPageHTMLPreservesExtractionMarkers(t *testing.T) {
	in := `<main><article class="entry-content" role="main"><p>body text</p></article></main>`
	out := PageHTML(in)
	for _, want := range []string{"<main>", `class="entry-content"`, `role="main"`, "body text"} {
		if !strings.Contains(out, want) {
			t.Fatalf("extraction marker %q stripped: %q", want, out)
		}
	}
}

func TestPageHTMLStillStripsScripts(t *testing.T) {
	in := `<main class="x"><script>alert(1)</script><img src=x onerror=alert(1)><p>ok</p></main>`
	out := PageHTML(in)
	if strings.Contains(out, "script") || strings.Contains(out, "onerror") || strings.Contains(out, "alert") {
		t.Fatalf("dangerous content survived page policy: %q", out)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("safe content removed: %q", out)
	}
}

func TestHTMLFeedPolicyStripsArbitraryClass(t *testing.T) {
	// Stored feed content keeps the stricter policy: class only on code/pre/span
	out := HTML(`<p class="icon-btn">text</p><code class="language-go">x</code>`)
	if strings.Contains(out, "icon-btn") {
		t.Fatalf("feed policy should strip class on p: %q", out)
	}
	if !strings.Contains(out, "language-go") {
		t.Fatalf("feed policy should keep class on code: %q", out)
	}
}
