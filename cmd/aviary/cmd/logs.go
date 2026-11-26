package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/logging"
)

var (
	logsFollow bool
	logsLines  int

	// logsFollowInterval is how often followFile polls the log file.
	logsFollowInterval = 350 * time.Millisecond
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail Aviary logs from the filesystem",
	RunE: func(cmd *cobra.Command, _ []string) error {
		path := logging.LogFilePath()
		if err := os.MkdirAll(logging.LogDir(), 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				_ = f.Close()
			}
		}

		if err := printTail(cmd.OutOrStdout(), path, logsLines); err != nil {
			return err
		}
		if !logsFollow {
			return nil
		}

		return followFile(cmd, path)
	},
}

func printTail(w io.Writer, path string, lines int) error {
	if lines <= 0 {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parts := strings.Split(string(b), "\n")
	start := 0
	if len(parts) > lines {
		start = len(parts) - lines
	}
	for _, ln := range parts[start:] {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		_, _ = fmt.Fprintln(w, ln)
	}
	return nil
}

func followFile(cmd *cobra.Command, path string) error {
	tail := &logTail{path: path, out: cmd.OutOrStdout()}
	if st, err := os.Stat(path); err == nil {
		tail.offset = st.Size()
	}

	ticker := time.NewTicker(logsFollowInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			tail.poll()
		}
	}
}

// logTail follows a log file from an offset, writing each complete,
// non-blank line it finds to out.
type logTail struct {
	path      string
	out       io.Writer
	offset    int64
	remainder string
}

// poll writes the lines appended since the last poll, starting over when the
// file shrank. Read errors are skipped until the next poll.
func (t *logTail) poll() {
	chunk := t.readNew()
	if len(chunk) == 0 {
		return
	}
	t.offset += int64(len(chunk))

	parts := strings.Split(t.remainder+string(chunk), "\n")
	t.remainder = parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part != "" {
			_, _ = fmt.Fprintln(t.out, part)
		}
	}
}

// readNew reads the bytes past the offset, resetting the offset and the
// partial line when the file was truncated.
func (t *logTail) readNew() []byte {
	f, err := os.Open(t.path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	if st.Size() < t.offset {
		t.offset = 0
		t.remainder = ""
	}
	if st.Size() == t.offset {
		return nil
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil
	}
	chunk, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return chunk
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 200, "number of trailing lines to show before follow")
	rootCmd.AddCommand(logsCmd)
}
