// Package exampleconfig embeds the example sessions, which the apps offer as
// a built-in session library.
package exampleconfig

import "embed"

// FS holds the example session files (*.yaml).
//
//go:embed *.yaml
var FS embed.FS
