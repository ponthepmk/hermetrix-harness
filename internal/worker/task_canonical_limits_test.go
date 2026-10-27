package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestEncodedCanonicalIsolation(t *testing.T) {
	limit := 16 * 1024 * 1024
	small := strings.Repeat("<", 1024*1024)
	files2 := map[string]string{"a": small, "b": small}
	files3 := map[string]string{"a": small, "b": small, "c": small}

	task2 := resourceFixtureTask(t, files2)
	canonical2, err := json.Marshal(task2)
	if err != nil {
		t.Fatalf("marshal N=2 task: %v", err)
	}
	if len(canonical2) >= limit {
		t.Fatalf("N=2 canonical %d not below %d", len(canonical2), limit)
	}
	snapshot2, preparedCanonical2, wire2, err := prepareWorkerTask(task2)
	if err != nil {
		t.Fatalf("prepareWorkerTask N=2: %v", err)
	}
	if preparedCanonical2 == nil {
		t.Fatalf("N=2 prepared canonical is nil")
	}
	if !bytes.Equal(preparedCanonical2, canonical2) {
		t.Fatalf("N=2 prepared canonical differs from marshaled canonical")
	}
	if reflect.DeepEqual(snapshot2, Task{}) {
		t.Fatalf("N=2 snapshot is zero")
	}
	if wire2 == nil {
		t.Fatalf("N=2 wire is nil")
	}

	task3 := resourceFixtureTask(t, files3)
	canonical3, err := json.Marshal(task3)
	if err != nil {
		t.Fatalf("marshal N=3 task: %v", err)
	}
	if len(canonical3) <= limit {
		t.Fatalf("N=3 canonical %d not above %d", len(canonical3), limit)
	}
	snapshot3, preparedCanonical3, wire3, err := prepareWorkerTask(task3)
	if err == nil {
		t.Fatalf("prepareWorkerTask N=3 expected error")
	}
	if preparedCanonical3 != nil {
		t.Fatalf("N=3 prepared canonical not nil")
	}
	if !reflect.DeepEqual(snapshot3, Task{}) {
		t.Fatalf("N=3 snapshot not zero")
	}
	if canonical3 == nil {
		t.Fatalf("N=3 canonical is nil")
	}
	if wire3 != nil {
		t.Fatalf("N=3 wire not nil")
	}
}
