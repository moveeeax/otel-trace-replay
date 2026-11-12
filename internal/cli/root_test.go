package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `{
  "resourceSpans": [
    {
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "checkout"}}]},
      "scopeSpans": [
        {
          "scope": {"name": "checkout/api"},
          "spans": [
            {"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "eee19b7ec3c1b174", "name": "POST /checkout", "startTimeUnixNano": "1700000000000000000", "endTimeUnixNano": "1700000000500000000"},
            {"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "eee19b7ec3c1b175", "parentSpanId": "eee19b7ec3c1b174", "name": "charge", "startTimeUnixNano": "1700000000100000000", "endTimeUnixNano": "1700000000300000000"}
          ]
        }
      ]
    }
  ]
}`

func writeFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "trace.json")
	if err := os.WriteFile(p, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDryRun(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--file", writeFixture(t), "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "loaded 2 span") {
		t.Fatalf("missing load line: %s", s)
	}
	if !strings.Contains(s, "would emit 2 span") {
		t.Fatalf("missing dry-run line: %s", s)
	}
}

func TestMissingFileFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when --file is missing")
	}
}

func TestNonexistentFile(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--file", "/no/such/trace.json", "--dry-run"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a nonexistent file")
	}
}

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDryRunNewIDsDefault(t *testing.T) {
	p := writeFixture(t)
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"single pass", []string{"--file", p, "--dry-run"}, false},
		{"times 2", []string{"--file", p, "--dry-run", "--times", "2"}, true},
		{"loop", []string{"--file", p, "--dry-run", "--loop"}, true},
		{"explicit off", []string{"--file", p, "--dry-run", "--times", "2", "--new-ids=false"}, false},
		{"explicit on", []string{"--file", p, "--dry-run", "--new-ids"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := execute(t, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(s, "new IDs for pass 1"); got != tc.want {
				t.Fatalf("mapping printed = %v, want %v:\n%s", got, tc.want, s)
			}
			if tc.want && (!strings.Contains(s, "trace 5b8efff798038103d269b633813fc60c -> ") ||
				!strings.Contains(s, "span  eee19b7ec3c1b175 -> ")) {
				t.Fatalf("mapping lines missing:\n%s", s)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	base := writeFixture(t)
	slow := filepath.Join(t.TempDir(), "slow.json")
	// charge: 200ms -> 580ms; the root ends 380ms later.
	s := strings.Replace(fixture, `"endTimeUnixNano": "1700000000300000000"`, `"endTimeUnixNano": "1700000000680000000"`, 1)
	s = strings.Replace(s, `"endTimeUnixNano": "1700000000500000000"`, `"endTimeUnixNano": "1700000000880000000"`, 1)
	if err := os.WriteFile(slow, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, "diff", "--baseline", base, "--candidate", slow, "--min-delta", "25ms")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(+380ms)") || !strings.Contains(out, "POST /checkout > charge") {
		t.Fatalf("unexpected diff output:\n%s", out)
	}

	out, err = execute(t, "diff", "-b", base, "-b", base, "-c", slow, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"path": "POST /checkout > charge"`) {
		t.Fatalf("unexpected json output:\n%s", out)
	}
}

func TestDiffErrors(t *testing.T) {
	p := writeFixture(t)
	for name, args := range map[string][]string{
		"missing candidate": {"diff", "--baseline", p},
		"bad format":        {"diff", "-b", p, "-c", p, "--format", "xml"},
		"negative delta":    {"diff", "-b", p, "-c", p, "--min-delta", "-1ms"},
		"missing file":      {"diff", "-b", p, "-c", "/no/such/trace.json"},
	} {
		if _, err := execute(t, args...); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}
