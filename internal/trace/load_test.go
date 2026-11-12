package trace

import (
	"encoding/hex"
	"testing"
)

const sample = `{
  "resourceSpans": [
    {
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "checkout"}}]},
      "scopeSpans": [
        {
          "scope": {"name": "checkout/api"},
          "spans": [
            {"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "eee19b7ec3c1b174", "name": "POST /checkout", "kind": 2, "startTimeUnixNano": "1700000000000000000", "endTimeUnixNano": "1700000000500000000"},
            {"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "eee19b7ec3c1b175", "name": "charge", "kind": 3, "startTimeUnixNano": "1700000000100000000", "endTimeUnixNano": "1700000000300000000"}
          ]
        }
      ]
    }
  ]
}`

func TestParse(t *testing.T) {
	td, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := CountSpans(td); got != 2 {
		t.Fatalf("CountSpans = %d, want 2", got)
	}
	rs := td.GetResourceSpans()
	if len(rs) != 1 {
		t.Fatalf("resource spans = %d, want 1", len(rs))
	}
	attrs := rs[0].GetResource().GetAttributes()
	if len(attrs) != 1 || attrs[0].GetKey() != "service.name" {
		t.Fatalf("resource attribute not parsed: %+v", attrs)
	}
	span := rs[0].GetScopeSpans()[0].GetSpans()[0]
	if span.GetName() != "POST /checkout" {
		t.Fatalf("span name = %q, want %q", span.GetName(), "POST /checkout")
	}
	if span.GetStartTimeUnixNano() != 1700000000000000000 {
		t.Fatalf("start = %d", span.GetStartTimeUnixNano())
	}
}

func TestParseInvalid(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestCountSpansEmpty(t *testing.T) {
	td, err := Parse([]byte(`{"resourceSpans": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := CountSpans(td); n != 0 {
		t.Fatalf("CountSpans = %d, want 0", n)
	}
}

func TestParseJSONLines(t *testing.T) {
	line := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"a"}]}]}]}`
	td, err := Parse([]byte(line + "\n\n" + line + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n := CountSpans(td); n != 2 {
		t.Fatalf("CountSpans = %d, want 2", n)
	}
}

func TestParseJSONLinesBadLine(t *testing.T) {
	line := `{"resourceSpans":[]}`
	if _, err := Parse([]byte(line + "\nnot json\n")); err == nil {
		t.Fatal("expected an error for an invalid line")
	}
}

func TestParseHexIDs(t *testing.T) {
	td, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	span := td.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	if got := hex.EncodeToString(span.GetTraceId()); got != "5b8efff798038103d269b633813fc60c" {
		t.Fatalf("traceId = %s (%d bytes), want the hex value from the file", got, len(span.GetTraceId()))
	}
	if got := hex.EncodeToString(span.GetSpanId()); got != "eee19b7ec3c1b174" {
		t.Fatalf("spanId = %s", got)
	}
}

func TestParseHexIDsInLinksAndParent(t *testing.T) {
	doc := `{"resourceSpans":[{"scopeSpans":[{"spans":[{
		"traceId":"5B8EFFF798038103D269B633813FC60C","spanId":"0000000000000002","parentSpanId":"0000000000000001",
		"links":[{"traceId":"0102030405060708090a0b0c0d0e0f10","spanId":"0a0b0c0d0e0f1011"}]}]}]}]}`
	td, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	sp := td.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	if hex.EncodeToString(sp.GetTraceId()) != "5b8efff798038103d269b633813fc60c" {
		t.Fatalf("upper-case hex traceId not decoded: %x", sp.GetTraceId())
	}
	if hex.EncodeToString(sp.GetParentSpanId()) != "0000000000000001" {
		t.Fatalf("parentSpanId = %x", sp.GetParentSpanId())
	}
	l := sp.GetLinks()[0]
	if hex.EncodeToString(l.GetTraceId()) != "0102030405060708090a0b0c0d0e0f10" || hex.EncodeToString(l.GetSpanId()) != "0a0b0c0d0e0f1011" {
		t.Fatalf("link IDs = %x/%x", l.GetTraceId(), l.GetSpanId())
	}
}
