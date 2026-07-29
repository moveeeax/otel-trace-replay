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
            {"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "eee19b7ec3c1b175", "name": "charge", "startTimeUnixNano": "1700000000100000000", "endTimeUnixNano": "1700000000300000000"}
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
