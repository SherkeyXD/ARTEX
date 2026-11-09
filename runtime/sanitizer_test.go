package runtime

import (
	"testing"
)

func TestSanitizeToolOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text unchanged",
			input:    "root:x:0:0:root:/root:/bin/bash\nbin:x:1:1:bin:/bin:/sbin/nologin\n",
			expected: "root:x:0:0:root:/root:/bin/bash\nbin:x:1:1:bin:/bin:/sbin/nologin\n",
		},
		{
			name:     "ANSI 16-colors stripped",
			input:    "\x1b[31m[CRITICAL]\x1b[0m \x1b[32m[PASS]\x1b[0m \x1b[33m[WARN]\x1b[0m",
			expected: "[CRITICAL] [PASS] [WARN]",
		},
		{
			name:     "ANSI 256-color and 24-bit truecolor stripped",
			input:    "\x1b[38;5;196mRedText\x1b[0m and \x1b[38;2;255;100;50mRGBText\x1b[0m",
			expected: "RedText and RGBText",
		},
		{
			name:     "ANSI cursor movement and clear line stripped",
			input:    "\x1b[?25lScanning ports...\x1b[2K\x1b[1GScan complete: 3 open ports.\n",
			expected: "Scanning ports...Scan complete: 3 open ports.\n",
		},
		{
			name:     "OSC terminal window title stripped",
			input:    "\x1b]0;Terminal Title\x07Output text goes here.",
			expected: "Output text goes here.",
		},
		{
			name:     "literal string \\x1b is preserved (not raw byte)",
			input:    "echo '\\x1b[0m' in bash script",
			expected: "echo '\\x1b[0m' in bash script",
		},
		{
			name:     "HTTP response with CRLF preserved as newlines",
			input:    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nServer: nginx\r\n\r\n{\"status\":\"ok\"}",
			expected: "HTTP/1.1 200 OK\nContent-Type: application/json\nServer: nginx\n\n{\"status\":\"ok\"}",
		},
		{
			name:     "terminal progress bar with standalone CR collapsed to final line",
			input:    "[    ] 0%\r[==  ] 50%\r[====] 100%\nDone!",
			expected: "[====] 100%\nDone!",
		},
		{
			name:     "progress bar with trailing CR and shorter final message",
			input:    "Downloading target asset: 99%\rDone!\r\nAll systems operational.\n",
			expected: "Done!\nAll systems operational.\n",
		},
		{
			name:     "SQL injection payload unchanged",
			input:    "admin' UNION SELECT 1, group_concat(schema_name), 3 FROM information_schema.schemata -- -",
			expected: "admin' UNION SELECT 1, group_concat(schema_name), 3 FROM information_schema.schemata -- -",
		},
		{
			name:     "XSS and HTML tags unchanged",
			input:    "<svg/onload=alert`xss`><iframe src=javascript:alert(1)>",
			expected: "<svg/onload=alert`xss`><iframe src=javascript:alert(1)>",
		},
		{
			name:     "Chinese characters with mixed ANSI colors",
			input:    "\x1b[1;31m【严重漏洞】\x1b[0m 发现目标存在未授权远程代码执行漏洞！\n\x1b[32m【修复建议】\x1b[0m 请立即升级组件。\n",
			expected: "【严重漏洞】 发现目标存在未授权远程代码执行漏洞！\n【修复建议】 请立即升级组件。\n",
		},
		{
			name:     "Base64 encoded string unchanged",
			input:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0",
			expected: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeToolOutput(tc.input)
			if got != tc.expected {
				t.Fatalf("SanitizeToolOutput(%q)\n  got:      %q\n  expected: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func BenchmarkSanitizeToolOutputPlain(b *testing.B) {
	input := "Plain text without any escape codes or carriage returns."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeToolOutput(input)
	}
}

func BenchmarkSanitizeToolOutputANSI(b *testing.B) {
	input := "\x1b[31;1mCRITICAL\x1b[0m \x1b[32m[PASS]\x1b[0m: Found target at \x1b[34mhttp://example.com/api\x1b[0m\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeToolOutput(input)
	}
}
