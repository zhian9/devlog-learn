package ingest

import (
	"strings"
	"testing"
)

func TestReadJSONLinesHandlesBOMBlankAndMissingFinalNewline(t *testing.T) {
	input := "\xEF\xBB\xBF{\"a\":1}\r\n\n{\"b\":2}\n{\"c\":3}"
	var got []JSONLine

	err := readJSONLines(strings.NewReader(input), 1024, func(line JSONLine) error {
		got = append(got, line)
		return nil
	})
	if err != nil {
		t.Fatalf("readJSONLines: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("lines = %d, want 3: %+v", len(got), got)
	}
	if got[0].No != 1 || string(got[0].Data) != `{"a":1}` {
		t.Fatalf("line 0 = %+v (%q)", got[0], got[0].Data)
	}
	if got[1].No != 3 || string(got[1].Data) != `{"b":2}` {
		t.Fatalf("line 1 = %+v (%q)", got[1], got[1].Data)
	}
	if got[2].No != 4 || string(got[2].Data) != `{"c":3}` {
		t.Fatalf("line 2 = %+v (%q)", got[2], got[2].Data)
	}
}

func TestReadJSONLinesTruncatesAndContinues(t *testing.T) {
	input := "{\"aaaa\":1}\n{\"b\":2}\n"
	var got []JSONLine

	err := readJSONLines(strings.NewReader(input), 8, func(line JSONLine) error {
		got = append(got, line)
		return nil
	})
	if err != nil {
		t.Fatalf("readJSONLines: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("lines = %d, want 2", len(got))
	}
	if !got[0].TooLong {
		t.Fatal("first line should be marked too long")
	}
	if len(got[0].Data) != 8 {
		t.Fatalf("truncated data length = %d, want 8", len(got[0].Data))
	}
	if got[0].Size != len("{\"aaaa\":1}\n") {
		t.Fatalf("reported size = %d", got[0].Size)
	}
	if got[1].TooLong || string(got[1].Data) != `{"b":2}` {
		t.Fatalf("second line = %+v (%q)", got[1], got[1].Data)
	}
}

func TestReadJSONLinesPropagatesCallbackError(t *testing.T) {
	want := strings.Repeat("x", 3)
	err := readJSONLines(strings.NewReader("a\n"), 1024, func(JSONLine) error {
		return &testError{msg: want}
	})
	if err == nil || err.Error() != want {
		t.Fatalf("callback error = %v, want %q", err, want)
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
