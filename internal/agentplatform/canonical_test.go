package agentplatform

import (
	"bytes"
	"testing"
)

func TestCanonicalJSONRejectsDuplicateAndNonContractNumbers(t *testing.T) {
	if _, err := CanonicalizeJSON([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("duplicate object key was accepted")
	}
	if _, err := CanonicalizeJSON([]byte(`{"a":1.5}`)); err == nil {
		t.Fatal("non-integer contract number was accepted")
	}
	if _, err := CanonicalizeJSON([]byte(`{"a":9007199254740992}`)); err == nil {
		t.Fatal("integer outside the JCS exact range was accepted")
	}
	got, err := CanonicalizeJSON([]byte(`{"\ue000":1,"😀":2,"a":"\u2028","z":-0}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("{\"a\":\"\u2028\",\"z\":0,\"😀\":2,\"\":1}")
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical JSON = %s, want %s", got, want)
	}
}

func TestSchemasCompileAtDraft202012(t *testing.T) {
	for _, name := range []string{"envelope", "task_assignment", "run_update", "ack"} {
		if _, err := compiledSchema(name); err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
	}
}
