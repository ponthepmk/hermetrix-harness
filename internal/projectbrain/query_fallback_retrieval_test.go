package projectbrain

import (
	"context"
	"strings"
	"testing"
)

func TestQueryFallbackRetrievesVerified(t *testing.T) {
	r, c := validFixture()
	good := c.pack
	c.pack = `{"status":"no_match","items":[]}`
	c.packByQuery = map[string]string{"load_record": good}
	q := strings.Repeat("please ", 16) + "ช่วยดู load_record ให้หน่อย"
	fragments, err := r.Retrieve(context.Background(), q)
	if err != nil || len(fragments) != 1 || len(c.calls) != 3 ||
		len(c.queries) != 2 || c.queries[1] != "load_record" || c.calls[2] != "read_passage" {
		t.Fatalf("fallback failed: err=%v fragments=%d calls=%v queries=%v", err, len(fragments), c.calls, c.queries)
	}
}

func TestQueryFallbackRejectsForeign(t *testing.T) {
	r, c := validFixture()
	good := strings.Replace(c.pack, `"client-a"`, `"client-b"`, 1)
	c.pack = `{"status":"no_match","items":[]}`
	c.packByQuery = map[string]string{"load_record": good}
	q := strings.Repeat("please ", 16) + "ช่วยดู load_record ให้หน่อย"
	fragments, err := r.Retrieve(context.Background(), q)
	if err == nil || len(fragments) != 0 || len(c.calls) != 2 {
		t.Fatalf("foreign fallback injected: err=%v fragments=%d calls=%v", err, len(fragments), c.calls)
	}
}
