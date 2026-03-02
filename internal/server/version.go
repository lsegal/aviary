package server

// Version is the server version string, injected at build time via:
//
//	go build -ldflags "-X github.com/lsegal/aviary/internal/server.Version=1.2.3"
//
// Falls back to "dev" for local builds.
var Version = "dev"
