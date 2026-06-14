package assets

import "embed"

//go:embed index.html login.html javascripts stylesheets
var FS embed.FS
