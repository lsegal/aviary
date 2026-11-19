// Command fakeglorp stands in for an installed glorp.exe in the Windows
// installer test.
//
// The installer only needs a binary it can ask for the skills targets and
// that can keep running while it is replaced, so this program answers those
// two and nothing else. Its version is set at build time with
// -ldflags "-X main.version=..." so each build the test installs is
// distinguishable.
//
//	fakeglorp agents -skills  prints one skills target
//	fakeglorp version         prints the build-time version
//	fakeglorp serve           runs until it is killed
package main

import (
	"fmt"
	"os"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeglorp agents -skills | version | serve")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "agents":
		fmt.Println("claude-code")
	case "version":
		fmt.Println(version)
	case "serve":
		for {
			time.Sleep(time.Hour)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}
