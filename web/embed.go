package web

import "embed"

// Assets contains the small frontend served by the Go API.
//
//go:embed index.html app.js styles.css
var Assets embed.FS
