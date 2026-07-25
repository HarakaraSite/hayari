package assets

import "embed"

//go:embed favicon.svg index.html login.html javascripts stylesheets
var FS embed.FS
