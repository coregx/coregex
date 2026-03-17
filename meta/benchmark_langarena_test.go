package meta

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// LangArena benchmark patterns from https://kostya.github.io/LangArena/
// Source: https://github.com/kostya/LangArena/blob/master/golang/main.go
//
// These are real-world patterns that expose Go regex weakness vs Rust/PCRE.
// Run with: go test -bench=BenchmarkLangArena -benchmem -count=3 ./meta/...

// logParserPatterns are the 13 patterns from LangArena Etc::LogParser benchmark.
var logParserPatterns = []struct {
	name    string
	pattern string
}{
	{"errors", ` [5][0-9]{2} | [4][0-9]{2} `},
	{"bots", `(?i)bot|crawler|scanner|spider|indexing|crawl|robot|spider`},
	{"suspicious", `(?i)etc/passwd|wp-admin|\.\./`},
	{"ips", `\d+\.\d+\.\d+\.35`},
	{"api_calls", `/api/[^ " ]+`},
	{"post_requests", `POST [^ ]* HTTP`},
	{"auth_attempts", `(?i)/login|/signin`},
	{"methods", `(?i)get|post|put`},
	{"emails", `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`},
	{"passwords", `password=[^&\s"]+`},
	{"tokens", `token=[^&\s"]+|api[_-]?key=[^&\s"]+`},
	{"sessions", `session[_-]?id=[^&\s"]+`},
	{"peak_hours", `\[\d+/\w+/\d+:1[3-7]:\d+:\d+ [+\-]\d+\]`},
}

// generateLogData generates realistic log data similar to LangArena's LogParser.
func generateLogData(linesCount int) string {
	ips := make([]string, 255)
	for i := range ips {
		ips[i] = fmt.Sprintf("192.168.1.%d", i+1)
	}
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	paths := []string{
		"/index.html", "/api/users", "/admin",
		"/images/logo.png", "/etc/passwd", "/wp-admin/setup.php",
	}
	statuses := []int{200, 201, 301, 302, 400, 401, 403, 404, 500, 502, 503}
	agents := []string{
		"Mozilla/5.0", "Googlebot/2.1", "curl/7.68.0", "scanner/2.0",
	}
	users := []string{
		"john", "jane", "alex", "sarah", "mike", "anna", "david", "elena",
	}
	domains := []string{
		"example.com", "gmail.com", "yahoo.com", "hotmail.com", "company.org", "mail.ru",
	}

	var b strings.Builder
	b.Grow(linesCount * 200)

	for i := 0; i < linesCount; i++ {
		b.WriteString(ips[i%len(ips)])
		b.WriteString(fmt.Sprintf(" - - [%d/Oct/2023:%d:55:36 +0000] \"", i%31, i%60))
		b.WriteString(methods[i%len(methods)])
		b.WriteString(" ")

		if i%3 == 0 {
			b.WriteString(fmt.Sprintf("/login?email=%s%d@%s&password=secret%d",
				users[i%len(users)], i%100,
				domains[i%len(domains)], i%10000))
		} else if i%5 == 0 {
			b.WriteString("/api/data?token=")
			for j := 0; j < (i%3)+1; j++ {
				b.WriteString("abcdef123456")
			}
		} else if i%7 == 0 {
			b.WriteString(fmt.Sprintf("/user/profile?session_id=sess_%x", i*12345))
		} else {
			b.WriteString(paths[i%len(paths)])
		}

		b.WriteString(fmt.Sprintf(" HTTP/1.1\" %d 2326 \"http://%s\" \"%s\"\n",
			statuses[i%len(statuses)],
			domains[i%len(domains)],
			agents[i%len(agents)]))
	}

	return b.String()
}

// generateTemplateData generates HTML template data similar to LangArena's Template::Regex.
func generateTemplateData(count int) (string, map[string]string) {
	firstNames := []string{"John", "Jane", "Bob", "Alice", "Charlie", "Diana", "Sarah", "Mike"}
	lastNames := []string{"Smith", "Johnson", "Brown", "Taylor", "Wilson", "Davis", "Miller", "Jones"}
	cities := []string{"New York", "Los Angeles", "Chicago", "Houston", "Phoenix", "San Francisco"}
	lorem := "Lorem {ipsum} dolor {sit} amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore {et} dolore magna aliqua. "

	vars := make(map[string]string)

	var buf strings.Builder
	buf.Grow(count * 200)

	buf.WriteString("<html><body>")
	buf.WriteString("<h1>{{TITLE}}</h1>")
	vars["TITLE"] = "Template title"
	buf.WriteString("<p>")
	buf.WriteString(lorem)
	buf.WriteString("</p>")
	buf.WriteString("<table>")

	for i := 0; i < count; i++ {
		if i%3 == 0 {
			buf.WriteString("<!-- {comment} -->")
		}
		buf.WriteString("<tr>")
		buf.WriteString(fmt.Sprintf("<td>{{ FIRST_NAME%d }}</td>", i))
		buf.WriteString(fmt.Sprintf("<td>{{LAST_NAME%d}}</td>", i))
		buf.WriteString(fmt.Sprintf("<td>{{  CITY%d  }}</td>", i))

		vars[fmt.Sprintf("FIRST_NAME%d", i)] = firstNames[i%len(firstNames)]
		vars[fmt.Sprintf("LAST_NAME%d", i)] = lastNames[i%len(lastNames)]
		vars[fmt.Sprintf("CITY%d", i)] = cities[i%len(cities)]

		buf.WriteString(fmt.Sprintf("<td>{balance: %d}</td>", i%100))
		buf.WriteString("</tr>\n")
	}

	buf.WriteString("</table>")
	buf.WriteString("</body></html>")

	return buf.String(), vars
}

// BenchmarkLangArenaLogParser benchmarks each of 13 LogParser patterns individually.
// Uses FindAllIndicesStreaming (coregex internal API) on generated log data.
func BenchmarkLangArenaLogParser(b *testing.B) {
	const linesCount = 10000
	log := generateLogData(linesCount)
	logBytes := []byte(log)
	b.Logf("log size: %d bytes (%.1f MB)", len(logBytes), float64(len(logBytes))/(1024*1024))

	for _, p := range logParserPatterns {
		b.Run(p.name, func(b *testing.B) {
			engine, err := Compile(p.pattern)
			if err != nil {
				b.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}
			b.Logf("strategy=%s", engine.Strategy())

			// Warm up to get match count
			results := engine.FindAllIndicesStreaming(logBytes, -1, nil)
			b.Logf("matches=%d", len(results))

			b.ResetTimer()
			b.SetBytes(int64(len(logBytes)))

			var buf [][2]int
			for i := 0; i < b.N; i++ {
				buf = engine.FindAllIndicesStreaming(logBytes, -1, buf[:0])
			}
		})
	}

	// Combined: all 13 patterns sequentially (simulates full LogParser run)
	b.Run("all_combined", func(b *testing.B) {
		engines := make([]*Engine, len(logParserPatterns))
		for i, p := range logParserPatterns {
			var err error
			engines[i], err = Compile(p.pattern)
			if err != nil {
				b.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}
		}

		b.ResetTimer()
		b.SetBytes(int64(len(logBytes)) * int64(len(logParserPatterns)))

		var buf [][2]int
		for i := 0; i < b.N; i++ {
			total := 0
			for _, engine := range engines {
				buf = engine.FindAllIndicesStreaming(logBytes, -1, buf[:0])
				total += len(buf)
			}
			_ = total
		}
	})
}

// BenchmarkLangArenaLogParserStdlib benchmarks the same patterns with Go stdlib.
func BenchmarkLangArenaLogParserStdlib(b *testing.B) {
	const linesCount = 10000
	log := generateLogData(linesCount)

	for _, p := range logParserPatterns {
		b.Run(p.name, func(b *testing.B) {
			re := regexp.MustCompile(p.pattern)

			b.ResetTimer()
			b.SetBytes(int64(len(log)))

			for i := 0; i < b.N; i++ {
				matches := re.FindAllStringIndex(log, -1)
				_ = matches
			}
		})
	}

	b.Run("all_combined", func(b *testing.B) {
		res := make([]*regexp.Regexp, len(logParserPatterns))
		for i, p := range logParserPatterns {
			res[i] = regexp.MustCompile(p.pattern)
		}

		b.ResetTimer()
		b.SetBytes(int64(len(log)) * int64(len(logParserPatterns)))

		for i := 0; i < b.N; i++ {
			total := 0
			for _, re := range res {
				matches := re.FindAllStringIndex(log, -1)
				total += len(matches)
			}
			_ = total
		}
	})
}

// BenchmarkLangArenaTemplateRegex benchmarks the Template::Regex pattern.
// Uses ReplaceAllStringFunc with {{(.*?)}} (same API as LangArena).
// Note: This uses the public Regex API (not Engine) since ReplaceAllStringFunc
// is on Regex, matching how kostya's try_coregex branch uses it.
func BenchmarkLangArenaTemplateRegex(b *testing.B) {
	const count = 5000
	text, vars := generateTemplateData(count)
	b.Logf("template size: %d bytes (%.1f KB), vars: %d", len(text), float64(len(text))/1024, len(vars))

	b.Run("coregex", func(b *testing.B) {
		engine, err := Compile(`\{\{(.*?)\}\}`)
		if err != nil {
			b.Fatal(err)
		}
		b.Logf("strategy=%s", engine.Strategy())

		// Use FindAllSubmatch to simulate ReplaceAllStringFunc workload
		// (Engine doesn't have ReplaceAllStringFunc directly)
		haystack := []byte(text)

		b.ResetTimer()
		b.SetBytes(int64(len(text)))

		for i := 0; i < b.N; i++ {
			matches := engine.FindAllSubmatch(haystack, -1)
			// Simulate the replace: for each match, look up var
			var result strings.Builder
			result.Grow(len(text))
			pos := 0
			for _, m := range matches {
				start, end := m.Start(), m.End()
				result.Write(haystack[pos:start])
				match := string(haystack[start:end])
				key := match[2 : len(match)-2]
				key = strings.TrimSpace(key)
				if val, ok := vars[key]; ok {
					result.WriteString(val)
				}
				pos = end
			}
			result.Write(haystack[pos:])
			_ = result.String()
		}
	})

	b.Run("stdlib", func(b *testing.B) {
		re := regexp.MustCompile(`\{\{(.*?)\}\}`)

		b.ResetTimer()
		b.SetBytes(int64(len(text)))

		for i := 0; i < b.N; i++ {
			result := re.ReplaceAllStringFunc(text, func(match string) string {
				key := match[2 : len(match)-2]
				key = strings.TrimSpace(key)
				if val, ok := vars[key]; ok {
					return val
				}
				return ""
			})
			_ = result
		}
	})
}
