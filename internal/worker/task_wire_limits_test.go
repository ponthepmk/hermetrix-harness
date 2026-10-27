package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerWireLowerBound(t *testing.T) {
	for _, size := range []int{20 * 1024, 25 * 1024} {
		source := strings.Repeat("<", size)
		task := resourceFixtureTask(t, map[string]string{"a": source})
		view, err := NewBoundedFileView("a", source, Hash(source), 0, len(source))
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		task.FileViews["a"] = []BoundedFileView{view}
		if err := validateWorkerWireLowerBound(task); err != nil {
			t.Fatalf("validateWorkerWireLowerBound: %v", err)
		}
		provider := task
		provider.Files = map[string]string{}
		providerJSON, err := json.Marshal(provider)
		if err != nil {
			t.Fatalf("json.Marshal provider: %v", err)
		}
		if size == 20*1024 {
			if len(providerJSON) > 128*1024 {
				t.Fatalf("20KiB serialized provider exceeds 128KiB")
			}
		} else if len(providerJSON) <= 128*1024 {
			t.Fatalf("25KiB serialized provider not greater than 128KiB")
		}
		snapshot, full, wire, err := prepareWorkerTask(task)
		if size == 20*1024 {
			if err != nil {
				t.Fatalf("prepareWorkerTask 20KiB: %v", err)
			}
			if reflect.DeepEqual(snapshot, Task{}) {
				t.Fatalf("snapshot nil")
			}
			if full == nil {
				t.Fatalf("full nil")
			}
			if !reflect.DeepEqual(wire, providerJSON) {
				t.Fatalf("wire differs from provider JSON")
			}
		} else {
			if err == nil {
				t.Fatalf("prepareWorkerTask 25KiB should fail")
			}
			if !reflect.DeepEqual(snapshot, Task{}) {
				t.Fatalf("snapshot not empty")
			}
			if full != nil || wire != nil {
				t.Fatalf("byte slices not nil")
			}
		}
	}
}
