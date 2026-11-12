package ingest

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// DefaultMaxLineBytes bounds one physical JSONL line. Session logs commonly
// contain multi-megabyte tool results, so the default is deliberately large.
const DefaultMaxLineBytes = 64 << 20

var errLineTooLong = errors.New("jsonl line exceeds maximum size")

// JSONLine is one physical line. Data is truncated to the configured maximum
// when TooLong is true; Size still reports the full byte count.
type JSONLine struct {
	No      int
	Data    []byte
	Size    int
	TooLong bool
}

// readJSONLines reads arbitrarily long physical lines without the 64 KiB
// bufio.Scanner limit. Blank lines are skipped. A line that exceeds maxLine is
// truncated, marked TooLong, and handed to fn so the caller can record a
// diagnosable warning instead of aborting the file.
func readJSONLines(r io.Reader, maxLine int, fn func(JSONLine) error) error {
	if maxLine <= 0 {
		maxLine = DefaultMaxLineBytes
	}

	br := bufio.NewReaderSize(r, 64*1024)
	lineNo := 0
	var buf []byte
	size := 0
	tooLong := false

	for {
		frag, err := br.ReadSlice('\n')
		if len(frag) > 0 {
			size += len(frag)

			content := frag
			if n := len(content); n > 0 && content[n-1] == '\n' {
				content = content[:n-1]
				if n = len(content); n > 0 && content[n-1] == '\r' {
					content = content[:n-1]
				}
			}

			remaining := maxLine - len(buf)
			if remaining < 0 {
				remaining = 0
			}
			take := len(content)
			if take > remaining {
				take = remaining
				tooLong = true
			}
			buf = append(buf, content[:take]...)
		}

		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return err
		}

		if size > 0 || tooLong {
			lineNo++
			data := buf
			if lineNo == 1 {
				data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
			}
			if len(bytes.TrimSpace(data)) > 0 || tooLong {
				line := JSONLine{No: lineNo, Data: data, Size: size, TooLong: tooLong}
				if cbErr := fn(line); cbErr != nil {
					return cbErr
				}
			}
			buf = nil
			size = 0
			tooLong = false
		}

		if err == io.EOF {
			return nil
		}
	}
}
