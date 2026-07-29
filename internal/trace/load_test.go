package trace

import "testing"

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
