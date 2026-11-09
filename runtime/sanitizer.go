package runtime

import (
	"regexp"
	"strings"
)

// ansiRegex matches ECMA-48 / VT100 ANSI escape sequences:
// 1. CSI (Control Sequence Introducer): \x1b\[[0-?]*[ -/]*[@-~]
//    (Includes SGR color/style codes \x1b[...m, cursor moves, clear line \x1b[2K, etc.)
// 2. OSC (Operating System Command): \x1b\][^\x07\x1b]*(?:\x07|\x1b\\)
//    (Includes terminal window title sequences, etc.)
// 3. DCS / PM / APC / SOS sequences: \x1b[PX^_][^\x1b]*(?:\x07|\x1b\\)
// 4. 2-character Fe escape sequences: \x1b[A-Z]
var ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[PX^_][^\x1b]*(?:\x07|\x1b\\)|\x1b[A-Z]`)

// SanitizeToolOutput cleans raw tool output for consumption by language models.
//
// Key safety and fidelity guarantees:
//  1. Fast path: If the string contains no non-printable ESC (0x1b) and no CR (0x0d),
//     it returns s directly with zero allocations.
//  2. ANSI stripping: Strictly matches raw 0x1b escape sequences; it never alters
//     printable string literals like "\\x1b" in source code, scripts, or PoC payloads.
//  3. Windows CRLF preservation: Converts \r\n to \n, preserving valid multiline structure
//     in HTTP headers, Windows files, and script outputs.
//  4. Standalone CR deduplication: Collapses terminal progress bar rewrites (isolated \r
//     without \n), retaining only the final line state to save tokens and eliminate noise.
func SanitizeToolOutput(s string) string {
	if s == "" {
		return ""
	}

	// 1. Fast path: check for control bytes ESC (0x1b) or CR (0x0d)
	if !strings.ContainsAny(s, "\x1b\r") {
		return s
	}

	// 2. Strip raw ANSI escape sequences
	if strings.ContainsRune(s, '\x1b') {
		s = ansiRegex.ReplaceAllString(s, "")
	}

	// If no CR remains, fast exit
	if !strings.ContainsRune(s, '\r') {
		return s
	}

	// 3. Normalize Windows CRLF (\r\n) to standard newline (\n).
	// This ensures HTTP response headers and Windows files remain intact as valid lines.
	s = strings.ReplaceAll(s, "\r\n", "\n")

	// If no standalone \r remains, we are done
	if !strings.ContainsRune(s, '\r') {
		return s
	}

	// 4. Handle standalone \r (terminal cursor return / progress bar overwrite)
	// In terminal semantics, \r without \n moves the cursor to column 0 to overwrite the line.
	// We retain the final state (the last non-empty segment) for each line.
	hasTrailingNewline := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	cleanedLines := make([]string, len(lines))

	for i, line := range lines {
		if !strings.ContainsRune(line, '\r') {
			cleanedLines[i] = line
			continue
		}

		parts := strings.Split(line, "\r")
		last := ""
		for j := len(parts) - 1; j >= 0; j-- {
			if strings.TrimSpace(parts[j]) != "" {
				last = parts[j]
				break
			}
		}
		if last == "" && len(parts) > 0 {
			last = parts[len(parts)-1]
		}
		cleanedLines[i] = last
	}

	result := strings.Join(cleanedLines, "\n")
	if hasTrailingNewline {
		result += "\n"
	}
	return result
}
