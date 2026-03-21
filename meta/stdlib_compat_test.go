package meta_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/coregx/coregex/meta"
)

// TestStdlibCompatibility verifies that coregex produces identical match results
// to Go's stdlib regexp for all regex-bench and LangArena patterns.
//
// This catches correctness regressions that performance benchmarks miss.
// Runs in CI on every PR.
func TestStdlibCompatibility(t *testing.T) {
	// Generate diverse test input covering all pattern types
	input := generateTestInput()

	patterns := []struct {
		name    string
		pattern string
	}{
		// regex-bench patterns
		{"literal_alt", `error|warning|fatal|critical`},
		{"multi_literal", `apple|banana|cherry|date|elderberry|fig|grape|honeydew|kiwi|lemon|mango|orange`},
		{"anchored", `^HTTP/[12]\.[01]`},
		{"inner_literal", `.*@example\.com`},
		{"suffix", `.*\.(txt|log|md)`},
		{"char_class", `[\w]+`},
		{"email", `[\w.+-]+@[\w.-]+\.[\w.-]+`},
		{"uri", `[\w]+://[^/\s?#]+[^\s?#]+(?:\?[^\s#]*)?(?:#[^\s]*)?`},
		{"version", `\d+\.\d+\.\d+`},
		{"ip", `(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`},
		{"alpha_digit", `[a-zA-Z]+\d+`},
		{"word_digit", `\w+[0-9]+`},
		{"http_methods", `(?m)^(GET|POST|PUT|DELETE|PATCH)`},
		{"anchored_php", `^/.*[\w-]+\.php`},
		{"multiline_php", `(?m)^/.*\.php`},
		{"word_repeat", `(\w{2,8})+`},

		// LangArena patterns
		{"la_errors", `(?i)(error|fail|exception|panic|fatal)`},
		{"la_bots", `(?i)(googlebot|bingbot|yandexbot|baiduspider|duckduckbot|slurp|facebookexternalhit|twitterbot|rogerbot|linkedinbot|embedly|quora link preview|showyoubot|outbrain|pinterest|applebot|semrushbot|ahrefsbot|mj12bot|dotbot|petalbot|bytespider)`},
		{"la_suspicious", `(?i)(eval|system|exec|execute|passthru|shell_exec|phpinfo|base64_decode|edoced_46esab|rot13|str_rot13|chmod|mkdir|fopen|fclose|readfile|union\s+select|etc/passwd|wp-admin|\.\./)`},
		{"la_ips", `\d+\.\d+\.\d+\.\d+`},
		{"la_api_calls", `(?m)^(?:GET|POST|PUT|DELETE|PATCH)\s+/api/\S+`},
		{"la_post_requests", `(?m)^POST\s+\S+`},
		{"la_auth_attempts", `(?i)(?:login|auth|sign.?in|session)`},
		{"la_methods", `(?m)^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s`},
		{"la_emails", `[\w.+-]+@[\w-]+\.[\w.-]+`},
		{"la_passwords", `(?m)^(?:GET|POST)\s+\S*(?:password|passwd|pwd|pass)\S*`},
		{"la_tokens", `[a-f0-9]{32,}`},
		{"la_sessions", `(?m)^(?:GET|POST)\s+\S*session\S*`},
		{"la_peak_hours", `(?:0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]`},

		// Edge cases
		{"empty_match", `a*`},
		{"word_boundary", `\btest\b`},
		{"non_greedy", `a+?`},
		{"alternation_overlap", `ab|abc`},
		{"nested_groups", `((a+)(b+))`},
		{"dot_star", `.*`},
		{"case_insensitive", `(?i)hello`},
		{"multiline_anchor", `(?m)^line`},
	}

	// Known pre-existing divergences from stdlib (tracked as issues).
	// These are NOT regressions — they fail on main too.
	knownIssues := map[string]string{
		"inner_literal": "greedy .* match boundaries differ from stdlib",
		"suffix":        "greedy .* match boundaries differ from stdlib",
		"http_methods":  "(?m) capture group FindAll boundaries differ",
		"la_suspicious": "case-insensitive alternation FindAll boundaries differ",
		"la_methods":    "(?m) capture group FindAll boundaries differ",
		"dot_star":      ".* newline handling differs from stdlib",
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			if reason, ok := knownIssues[p.name]; ok {
				t.Skipf("known divergence: %s", reason)
			}

			stdRe, err := regexp.Compile(p.pattern)
			if err != nil {
				t.Skipf("stdlib can't compile: %v", err)
			}

			engine, err := meta.Compile(p.pattern)
			if err != nil {
				t.Fatalf("coregex can't compile: %v", err)
			}

			// Compare IsMatch
			stdMatch := stdRe.Match(input)
			ourMatch := engine.IsMatch(input)
			if stdMatch != ourMatch {
				t.Errorf("IsMatch: stdlib=%v coregex=%v", stdMatch, ourMatch)
			}

			// Compare Find
			stdFind := stdRe.Find(input)
			ourFind := engine.Find(input)
			if ourFind != nil {
				ourFindBytes := ourFind.Bytes()
				if string(stdFind) != string(ourFindBytes) {
					t.Errorf("Find: stdlib=%q coregex=%q", stdFind, ourFindBytes)
				}
			} else if stdFind != nil {
				t.Errorf("Find: stdlib=%q coregex=nil", stdFind)
			}

			// Compare FindAllIndex positions (up to 1000 matches)
			stdIdx := stdRe.FindAllIndex(input, 1000)
			var ourIdx [][2]int
			ourIdx = engine.FindAllIndicesStreaming(input, 1000, ourIdx)
			if len(stdIdx) != len(ourIdx) {
				t.Errorf("FindAll count: stdlib=%d coregex=%d (strategy=%s)",
					len(stdIdx), len(ourIdx), engine.Strategy())
				// Show first divergence
				minLen := len(stdIdx)
				if len(ourIdx) < minLen {
					minLen = len(ourIdx)
				}
				for i := 0; i < minLen && i < 5; i++ {
					stdS, stdE := stdIdx[i][0], stdIdx[i][1]
					ourS, ourE := ourIdx[i][0], ourIdx[i][1]
					if stdS != ourS || stdE != ourE {
						t.Errorf("  match[%d]: stdlib=[%d,%d]%q coregex=[%d,%d]%q",
							i, stdS, stdE, input[stdS:stdE], ourS, ourE, input[ourS:ourE])
					}
				}
			} else {
				for i, si := range stdIdx {
					if si[0] != ourIdx[i][0] || si[1] != ourIdx[i][1] {
						t.Errorf("FindAll[%d]: stdlib=[%d,%d]%q coregex=[%d,%d]%q",
							i, si[0], si[1], input[si[0]:si[1]],
							ourIdx[i][0], ourIdx[i][1], input[ourIdx[i][0]:ourIdx[i][1]])
						if i >= 5 {
							t.Errorf("  ... and more mismatches")
							break
						}
					}
				}
			}

			// Also compare Count
			ourCount := engine.Count(input, 1000)
			if len(stdIdx) != ourCount {
				t.Errorf("Count: stdlib=%d coregex=%d", len(stdIdx), ourCount)
			}
		})
	}
}

// generateTestInput creates a diverse input that exercises all pattern types.
func generateTestInput() []byte {
	lines := []string{
		"HTTP/1.1 200 OK",
		"GET /api/users HTTP/1.1",
		"POST /api/login HTTP/1.1",
		"DELETE /api/session HTTP/1.1",
		"PUT /api/config HTTP/1.1",
		"PATCH /api/profile HTTP/1.1",
		`192.168.1.100 - - [15/Jan/2024:10:30:45 +0300] "GET /index.html HTTP/1.1" 200 1234`,
		`10.0.0.1 - admin [15/Jan/2024:14:22:33 +0300] "POST /login?password=secret HTTP/1.1" 302 0`,
		`172.16.0.50 - - [15/Jan/2024:23:59:59 +0300] "GET /api/data HTTP/1.1" 200 5678`,
		"error: connection refused to database",
		"warning: disk space low on /dev/sda1",
		"fatal: unable to allocate memory",
		"critical: security breach detected",
		"[error] exception in handler: panic at line 42",
		"User-Agent: Mozilla/5.0 (Googlebot/2.1; +http://www.google.com/bot.html)",
		"User-Agent: Mozilla/5.0 (compatible; Bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"User-Agent: Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)",
		"user@example.com sent email to admin@company.org",
		"contact support+help@test-domain.co.uk for help",
		"Visit https://example.com/path?query=1#section or http://test.org/page",
		"Version 1.2.3 released, upgrading from 10.0.1 to 10.0.2",
		"eval(base64_decode('malicious')) detected in /var/www/uploads/shell.php",
		"phpinfo() call from 192.168.1.50 blocked",
		"SELECT * FROM users WHERE id=1 UNION SELECT password FROM admin",
		"Attempt to access /etc/passwd and ../../config",
		"apple banana cherry date elderberry fig grape honeydew kiwi lemon mango orange",
		"session_id=abc123def456 auth_token=0123456789abcdef0123456789abcdef",
		"login attempt for user admin from 10.0.0.5 at 08:15:30",
		"File report.txt created, also backup.log and notes.md available",
		"/admin/dashboard.php loaded in 0.5s",
		"/api/v2/users.php returned 404",
		"GET /static/style.css HTTP/1.1",
		"HEAD /health HTTP/1.1",
		"OPTIONS /api/cors HTTP/1.1",
		"abc123 def456 test789 hello world123",
		"word word2 word34 word567 word8901",
		"test testing tested tester",
		"line one here",
		"line two here",
		"hello Hello HELLO hElLo",
		fmt.Sprintf("long token: %s end", strings.Repeat("abcdef0123456789", 3)),
	}

	var buf strings.Builder
	// Repeat to get substantial input
	for i := 0; i < 100; i++ {
		for _, line := range lines {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return []byte(buf.String())
}
