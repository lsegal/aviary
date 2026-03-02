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

		if err := printTail(path, logsLines); err != nil {
			return err
		}
		if !logsFollow {
			return nil
		}

		return followFile(cmd, path)
	},
}

func printTail(path string, lines int) error {
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
		fmt.Println(ln)
	}
	return nil
}

func followFile(cmd *cobra.Command, path string) error {
	var offset int64
	if st, err := os.Stat(path); err == nil {
		offset = st.Size()
	}

	var remainder string
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			st, err := f.Stat()
			if err != nil {
				_ = f.Close()
				continue
			}
			if st.Size() < offset {
				offset = 0
				remainder = ""
			}
			if st.Size() == offset {
				_ = f.Close()
				continue
			}
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				_ = f.Close()
				continue
			}
			chunk, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil || len(chunk) == 0 {
				continue
			}
			offset += int64(len(chunk))

			text := remainder + string(chunk)
			parts := strings.Split(text, "\n")
			remainder = parts[len(parts)-1]
			for _, part := range parts[:len(parts)-1] {
				part = strings.TrimSpace(part)
				if part != "" {
					fmt.Println(part)
				}
			}
		}
	}
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 200, "number of trailing lines to show before follow")
	rootCmd.AddCommand(logsCmd)
}
