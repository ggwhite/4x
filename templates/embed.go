package templates

import "embed"

//go:embed *.tmpl
var FS embed.FS

//go:embed profiles/*.md
var ProfilesFS embed.FS
