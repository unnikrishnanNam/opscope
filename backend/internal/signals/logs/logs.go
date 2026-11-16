// Package logs reads a little of a failing container's log and says what
// it found, without keeping any of it: which known patterns matched and how
// many times, and how many other kinds of line there were. The lines, and
// the templates made from them, are dropped when Match returns (contract
// 11). See docs/design/issues.md, "Logs".
package logs

import (
	"bufio"
	"io"
	"regexp"
	"slices"
	"strings"
)

// Limits on what's read of one container's log.
const (
	MaxLines = 200       // the last lines asked for (the API's tailLines)
	MaxBytes = 64 * 1024 // and never more than this, whichever comes first
)

// Pattern is a known kind of log line. Patterns are data, so a rule pack
// (v1.13) can add its own.
type Pattern struct {
	Name  string // e.g. "connection_refused"
	About string // in words, for messages: "connection refused"
	Match *regexp.Regexp
}

// Patterns are the built-in ones. Phrases match in any case; error codes
// (ECONNREFUSED, EACCES, …) only as whole words in capitals, since "eacces"
// is inside "HandleAccessor". Each has an example in the tests, and lines
// that look close and mustn't match.
var Patterns = []Pattern{
	{"connection_refused", "connection refused", regexp.MustCompile(`(?i:connection refused)|\bECONNREFUSED\b`)},
	{"auth_failed", "refused credentials", regexp.MustCompile(
		`(?i:password authentication failed|authentication failed|access denied for user|invalid credentials|invalid username or password)`)},
	{"no_such_file", "a missing file", regexp.MustCompile(`(?i:no such file or directory)|\bENOENT\b|\bFileNotFoundException\b`)},
	{"permission_denied", "permission denied", regexp.MustCompile(`(?i:permission denied|operation not permitted)|\bEACCES\b|\bEPERM\b`)},
	{"out_of_memory", "out of memory", regexp.MustCompile(
		`(?i:out of memory|cannot allocate memory)|\bjava\.lang\.OutOfMemoryError\b|\bOutOfMemoryError:`)},
	{"address_in_use", "an address already in use", regexp.MustCompile(`(?i:address already in use)|\bEADDRINUSE\b`)},
	{"tls_failure", "a TLS failure", regexp.MustCompile(`(?i:tls: |x509: |certificate (has expired|is not (yet )?valid|signed by unknown)|handshake fail)`)},
	{"dns_failure", "a name that doesn't resolve", regexp.MustCompile(
		`(?i:no such host|name or service not known|temporary failure in name resolution|could not resolve host)|\bENOTFOUND\b|\bUnknownHostException\b`)},
	{"timeout", "a timeout", regexp.MustCompile(`(?i:i/o timeout|deadline exceeded|timed out)|\bETIMEDOUT\b`)},
	{"crash", "a crash (panic or uncaught exception)", regexp.MustCompile(
		`^panic: |goroutine \d+ \[running\]|Traceback \(most recent call last\)|(?i:unhandled exception|uncaught exception)|Exception in thread "`)},
}

// Result is what one read found. It holds no text from the log.
type Result struct {
	Lines    int            // lines read
	Patterns map[string]int // pattern name → lines it matched
	// Other is how many kinds of line matched no pattern: lines that are
	// the same once numbers, addresses, IDs and quoted text are taken out
	// count as one kind. v1.12 can ask about them; the kinds themselves
	// aren't kept.
	Other int
}

// maxLine is how much of one line is looked at; the rest of a longer line
// is skipped. A Java service logging JSON puts a whole stack trace, with
// its "Caused by", on one line of several KiB.
const maxLine = 16 * 1024

// Match reads at most MaxBytes from r and matches every line against
// patterns. A line can match more than one pattern.
func Match(r io.Reader, patterns []Pattern) (Result, error) {
	res := Result{Patterns: map[string]int{}}
	other := map[string]bool{}
	br := bufio.NewReaderSize(io.LimitReader(r, MaxBytes), maxLine)
	for {
		raw, err := br.ReadSlice('\n')
		line := strings.TrimRight(string(raw), "\r\n")
		for err == bufio.ErrBufferFull { // a long line: look at its start only
			_, err = br.ReadSlice('\n')
		}
		if strings.TrimSpace(line) != "" {
			res.Lines++
			matched := false
			for _, p := range patterns {
				if p.Match.MatchString(line) {
					res.Patterns[p.Name]++
					matched = true
				}
			}
			if !matched {
				other[template(line)] = true
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, err
		}
	}
	res.Other = len(other)
	return res, nil
}

var (
	quoted   = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	uuid     = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	address  = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b|\[?[0-9a-fA-F]*:[0-9a-fA-F:]{2,}\]?(:\d+)?`)
	hex      = regexp.MustCompile(`(?i)\b(0x)?[0-9a-f]{8,}\b`)
	number   = regexp.MustCompile(`\d+`)
	spaceRun = regexp.MustCompile(`\s+`)
)

// template reduces a line to its kind, Drain-style: quoted text, UUIDs,
// addresses, long hex and numbers become placeholders, so "took 12ms" and
// "took 340ms" are one kind.
func template(line string) string {
	line = quoted.ReplaceAllString(line, `"*"`)
	line = uuid.ReplaceAllString(line, "<id>")
	line = address.ReplaceAllString(line, "<addr>")
	line = hex.ReplaceAllString(line, "<hex>")
	line = number.ReplaceAllString(line, "<n>")
	return spaceRun.ReplaceAllString(strings.TrimSpace(line), " ")
}

// About is a pattern's words, or its name for one not in patterns.
func About(name string) string {
	if i := slices.IndexFunc(Patterns, func(p Pattern) bool { return p.Name == name }); i >= 0 {
		return Patterns[i].About
	}
	return strings.ReplaceAll(name, "_", " ")
}
