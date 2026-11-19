package logs

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	log := strings.Join([]string{
		"2026-11-16T10:00:00Z starting order-service v2.3.1",
		"connecting to postgres at 10.96.0.12:5432",
		`dial tcp 10.96.0.12:5432: connect: connection refused`,
		`dial tcp 10.96.0.13:5432: connect: connection refused`,
		"open /etc/app/config.yaml: no such file or directory",
		"panic: runtime error: invalid memory address or nil pointer dereference",
		"goroutine 1 [running]:",
		"request 7f3a9c2e-1b4d-4e8a-9c3f-2a1b3c4d5e6f took 12ms",
		"request 0b1c2d3e-4f5a-6b7c-8d9e-0f1a2b3c4d5e took 340ms",
		"",
	}, "\n")
	got, err := Match(strings.NewReader(log), Patterns)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Lines: 9, Patterns: map[string]int{"connection_refused": 2, "no_such_file": 1, "crash": 2},
		// "starting …", "connecting to …" and the two requests, which are one kind.
		Other: 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMatchReadsAtMostTheLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; b.Len() < 3*MaxBytes; i++ {
		fmt.Fprintf(&b, "line %d: connection refused\n", i)
	}
	got, err := Match(strings.NewReader(b.String()), Patterns)
	if err != nil {
		t.Fatal(err)
	}
	shortest, longest := len("line 0: connection refused\n"), len("line 9999: connection refused\n")
	if got.Lines > MaxBytes/shortest+1 || got.Lines < MaxBytes/longest {
		t.Errorf("read %d lines of %d bytes", got.Lines, b.Len())
	}

	// A line longer than maxLine is looked at by its start, and the lines
	// after it are still read.
	long := "connection refused " + strings.Repeat("x", 2*maxLine) + "\nno such file or directory\n"
	got, err = Match(strings.NewReader(long), Patterns)
	if err != nil || got.Lines != 2 || got.Patterns["connection_refused"] != 1 || got.Patterns["no_such_file"] != 1 {
		t.Errorf("a long line: %+v, %v", got, err)
	}
}

func TestEveryPatternMatchesItsExample(t *testing.T) {
	examples := map[string]string{
		"connection_refused": "Error: connect ECONNREFUSED 127.0.0.1:6379",
		"no_such_file":       "java.io.FileNotFoundException: /data/app.db",
		"permission_denied":  "mkdir /var/lib/app: permission denied",
		"out_of_memory":      "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory",
		"address_in_use":     "listen tcp :8080: bind: address already in use",
		"tls_failure":        "x509: certificate signed by unknown authority",
		"dns_failure":        "dial tcp: lookup db.shop.svc on 10.96.0.10:53: no such host",
		"timeout":            "context deadline exceeded",
		"auth_failed":        `Caused by: org.postgresql.util.PSQLException: FATAL: password authentication failed for user "orders"`,
		"crash":              "Traceback (most recent call last):",
	}
	for _, p := range Patterns {
		ex, ok := examples[p.Name]
		if !ok {
			t.Errorf("%s has no example", p.Name)
			continue
		}
		if !p.Match.MatchString(ex) {
			t.Errorf("%s doesn't match %q", p.Name, ex)
		}
		if p.About == "" {
			t.Errorf("%s has no words", p.Name)
		}
	}
	// Lines that look close, from a real Java service's log, and match nothing.
	for _, near := range []string{
		`at java.base/jdk.internal.reflect.DirectMethodHandleAccessor.invoke(Unknown Source)`, // "eacces"
		`Picked up JAVA_TOOL_OPTIONS: -XX:MaxRAMPercentage=70 -XX:+ExitOnOutOfMemoryError`,    // a flag, not an error
		`Tomcat initialized with port 8080 (http)`,
		`Connection to nats opened; reconnect on timeoutless close`,
	} {
		for _, p := range Patterns {
			if p.Match.MatchString(near) {
				t.Errorf("%s matches %q", p.Name, near)
			}
		}
	}
	if !strings.Contains(About("connection_refused"), "refused") || About("made_up") != "made up" {
		t.Error("About")
	}
}

func TestTemplates(t *testing.T) {
	same := [][2]string{
		{"took 12ms", "took 340ms"},
		{`user "alice" logged in from 10.0.0.1`, `user "bob" logged in from 192.168.1.20`},
		{"trace 7f3a9c2e1b4d4e8a", "trace 0b1c2d3e4f5a6b7c"},
	}
	for _, p := range same {
		if template(p[0]) != template(p[1]) {
			t.Errorf("%q and %q differ: %q, %q", p[0], p[1], template(p[0]), template(p[1]))
		}
	}
	if template("starting") == template("stopping") {
		t.Error("different words are one kind")
	}
}
