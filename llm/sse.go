package llm

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// sseScanner reads Server-Sent Events from a response body, yielding the
// (event, data) pair of each message. Anthropic uses named events; OpenAI uses
// bare data lines. Both are handled.
type sseScanner struct {
	r           *bufio.Reader
	terminalErr error
}

func newSSEScanner(body io.Reader) *sseScanner {
	return &sseScanner{r: bufio.NewReaderSize(body, 64*1024)}
}

// next returns the next event's (type, data). event is "" for bare-data streams.
// Returns io.EOF at end of stream.
func (s *sseScanner) next() (event, data string, err error) {
	if s.terminalErr != nil {
		return "", "", s.terminalErr
	}
	var buf bytes.Buffer
	hasData := false
	for {
		line, rerr := s.r.ReadString('\n')
		// ReadString may return bytes together with EOF. Parse that final line
		// before ending the event, while retaining other errors for the caller.
		if rerr != nil {
			s.terminalErr = rerr
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if hasData || event != "" {
				return event, strings.TrimSuffix(buf.String(), "\n"), nil
			}
			if rerr != nil {
				return "", "", rerr
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			hasData = true
			d := line[len("data:"):]
			d = strings.TrimPrefix(d, " ")
			buf.WriteString(d)
			buf.WriteByte('\n')
		default:
			// comment / unknown field — ignore
		}
		if rerr != nil {
			if rerr == io.EOF && (hasData || event != "") {
				return event, strings.TrimSuffix(buf.String(), "\n"), nil
			}
			return "", "", rerr
		}
	}
}
