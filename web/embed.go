package web

import "embed"

//go:embed index.html app.js style.css i18n.js
var FS embed.FS
