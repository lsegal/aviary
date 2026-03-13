// Package templates exposes embedded filesystem templates used to initialize
// runtime artifacts.
package templates

import (
	"embed"
	"io/fs"
)

//go:embed all:agent
var files embed.FS

// Agent returns the embedded agent template tree rooted at support/templates/agent.
func Agent() fs.FS {
	sub, err := fs.Sub(files, "agent")
	if err != nil {
		panic(err)
	}
	return sub
}
