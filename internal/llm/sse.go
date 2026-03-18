package llm

import (
	"bufio"
	"io"
)

const maxSSELineBytes = 1024 * 1024

func newSSEScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	return scanner
}
