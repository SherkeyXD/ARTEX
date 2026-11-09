package llm

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSEFinalLineAndEventBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, input, event, data string }{
		{"bare tail", "data: tail", "", "tail"},
		{"named tail", "event: delta\ndata: tail", "delta", "tail"},
		{"multiline tail", "data: first\ndata: second", "", "first\nsecond"},
		{"empty data", "data:", "", ""},
		{"preserve empty trailing data", "data: first\ndata:\n\n", "", "first\n"},
		{"CRLF", "event: delta\r\ndata: tail\r\n\r\n", "delta", "tail"},
		{"comments", "\n: keepalive\ndata: tail", "", "tail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanner := newSSEScanner(strings.NewReader(tc.input))
			event, data, err := scanner.next()
			if err != nil || event != tc.event || data != tc.data {
				t.Fatalf("event=%q data=%q err=%v", event, data, err)
			}
			if _, _, err := scanner.next(); !errors.Is(err, io.EOF) {
				t.Fatalf("after tail err=%v", err)
			}
		})
	}
}

func TestSSEFinalEventNotRepeated(t *testing.T) {
	scanner := newSSEScanner(strings.NewReader("data: first\n\ndata: last"))
	for _, want := range []string{"first", "last"} {
		_, got, err := scanner.next()
		if err != nil || got != want {
			t.Fatalf("got=%q want=%q err=%v", got, want, err)
		}
	}
	for range 2 {
		if _, _, err := scanner.next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
}

type brokenSSEReader struct{}

func (brokenSSEReader) Read(p []byte) (int, error) {
	return copy(p, "data: incomplete"), io.ErrUnexpectedEOF
}
func TestSSETruncatedTransportIsError(t *testing.T) {
	_, _, err := newSSEScanner(brokenSSEReader{}).next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("transport error swallowed: %v", err)
	}
}
