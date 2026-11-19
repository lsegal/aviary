// Command fakeagent stands in for a vendor agent CLI in glorp's own tests.
//
// The real CLIs cannot be exercised in CI: they are not installed on the
// runners and most of them need credentials, so a definition's claim about the
// argv, environment, session ID, and output stream of the agent it describes
// would otherwise be untested. This program is invoked in their place. It
// records every invocation it receives -- argv, working directory, and the
// environment variables it was told to watch -- as one JSON object per line in
// the file named by GLORP_FAKE_AGENT_RECORD, and then behaves as that
// invocation was configured to.
//
// Everything it does is driven by the environment rather than by flags,
// because the argv is the thing under test and has to arrive exactly as the
// definition rendered it.
//
//	GLORP_FAKE_AGENT_RECORD   file the invocation records are appended to (required)
//	GLORP_FAKE_AGENT_STDOUT   text written to stdout, with \n understood
//	GLORP_FAKE_AGENT_SESSION  session ID announced as "session id: <value>"
//	GLORP_FAKE_AGENT_CHECKOUT directory announced as GLORP_CHECKOUT_DIRECTORY=
//	GLORP_FAKE_AGENT_ENV      comma-separated variables to record
//	GLORP_FAKE_AGENT_MISSING  0-based invocation that reports a missing session
//	GLORP_FAKE_AGENT_MISSING_TEXT  what that invocation prints, so a definition
//	                          naming its own missing-session phrase is exercised
//	GLORP_FAKE_AGENT_FAIL     0-based invocation that exits non-zero
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// record is one invocation, as the test reads it back.
type record struct {
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Env  map[string]string `json:"env"`
}

func main() {
	path := os.Getenv("GLORP_FAKE_AGENT_RECORD")
	if path == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: GLORP_FAKE_AGENT_RECORD is required")
		os.Exit(2)
	}
	index, err := appendRecord(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		os.Exit(2)
	}
	if session := os.Getenv("GLORP_FAKE_AGENT_SESSION"); session != "" {
		fmt.Printf("session id: %s\n", session)
	}
	if checkout := os.Getenv("GLORP_FAKE_AGENT_CHECKOUT"); checkout != "" {
		fmt.Printf("GLORP_CHECKOUT_DIRECTORY=%s\n", checkout)
	}
	if text := os.Getenv("GLORP_FAKE_AGENT_STDOUT"); text != "" {
		fmt.Println(strings.ReplaceAll(text, `\n`, "\n"))
	}
	if matchesInvocation("GLORP_FAKE_AGENT_MISSING", index) {
		// The wording is one of the messages glorp watches for when a resume
		// names a session the agent no longer holds, or the one the agent's
		// own definition says it prints instead.
		missing := os.Getenv("GLORP_FAKE_AGENT_MISSING_TEXT")
		if missing == "" {
			missing = "error: session not found"
		}
		fmt.Fprintln(os.Stderr, missing)
		os.Exit(1)
	}
	if matchesInvocation("GLORP_FAKE_AGENT_FAIL", index) {
		fmt.Fprintln(os.Stderr, "fakeagent: failing as instructed")
		os.Exit(3)
	}
}

// appendRecord writes this invocation to the record file and returns how many
// invocations came before it.
func appendRecord(path string) (int, error) {
	index, err := countRecords(path)
	if err != nil {
		return 0, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	entry := record{Args: os.Args[1:], Dir: dir, Env: map[string]string{}}
	for _, name := range strings.Split(os.Getenv("GLORP_FAKE_AGENT_ENV"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			entry.Env[name] = os.Getenv(name)
		}
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return 0, err
	}
	return index, nil
}

func countRecords(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, scanner.Err()
}

// matchesInvocation reports whether a behaviour configured for a particular
// 0-based invocation applies to this one.
func matchesInvocation(name string, index int) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	want, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	return want == index
}
