package ngrok

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultBinary is the ngrok command glorp runs when the user configured none
// of their own. It is a bare name rather than a path because it is looked up
// on PATH first and, failing that, fetched through npx (issue #498).
const DefaultBinary = "ngrok"

// npxBinary and npxPackage name the npm entry point glorp falls back to. The
// `ngrok` package's bin is the ngrok agent itself rather than a Node shim, so
// running it this way starts the same process a local install would, which is
// what keeps the log parsing and the orphan sweep working unchanged.
const (
	npxBinary  = "npx"
	npxPackage = "ngrok"
)

const (
	// startTimeout is how long an already-installed agent gets to report its
	// tunnel.
	startTimeout = 10 * time.Second
	// npxStartTimeout covers the first npx run, which downloads the npm
	// package and the agent binary before ngrok itself starts. Ten seconds is
	// not enough for that on a cold cache, and a timeout there would look to
	// the user like a broken tunnel rather than a slow download.
	npxStartTimeout = 2 * time.Minute
)

// lookPath is a variable so tests can decide what is installed.
var lookPath = exec.LookPath

// command is how glorp invokes the ngrok agent: the executable to run and the
// arguments that precede ngrok's own.
type command struct {
	name   string
	prefix []string
	viaNpx bool
}

// timeout is how long this invocation gets to bring a tunnel up.
func (c command) timeout() time.Duration {
	if c.viaNpx {
		return npxStartTimeout
	}
	return startTimeout
}

// resolveCommand decides how to run ngrok for the configured binary.
//
// An installed executable always wins, so a user who has ngrok on PATH keeps
// the exact behaviour, and the startup cost, they had before. npx is used only
// for the default binary: a `--ngrok-binary` naming something else is an
// explicit choice about which agent to run, and quietly substituting a
// downloaded one when it is missing would hide the user's typo instead of
// reporting it.
func resolveCommand(binary string) (command, error) {
	if binary == "" {
		binary = DefaultBinary
	}
	if _, err := lookPath(binary); err == nil {
		return command{name: binary}, nil
	}
	if binary != DefaultBinary {
		return command{}, fmt.Errorf("start ngrok: %q not found; install it or correct -ngrok-binary", binary)
	}
	if _, err := lookPath(npxBinary); err != nil {
		return command{}, fmt.Errorf("start ngrok: no %q executable and no %q to fetch one; install ngrok (https://ngrok.com/) or Node.js (https://nodejs.org/)", binary, npxBinary)
	}
	return command{name: npxBinary, prefix: []string{"--yes", npxPackage}, viaNpx: true}, nil
}

// args is the full argument list for the invocation, ngrok's own arguments
// included.
func (c command) args(listenAddr string) []string {
	return append(append([]string{}, c.prefix...), ngrokArgs(listenAddr)...)
}

// npmProgress reports whether a line is npm's own chatter rather than anything
// ngrok said. Fetching the package through npx puts deprecation warnings and
// progress notices on the same streams glorp reads the tunnel from, and the
// watcher would otherwise take every one of them for an unclassifiable ngrok
// failure and print it. Only npm's advisory prefixes are dropped: `npm error`
// stays a failure, because a package that cannot be fetched is exactly why no
// tunnel came up.
func npmProgress(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"npm warn", "npm notice", "npm WARN", "npm http", "npm info"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
