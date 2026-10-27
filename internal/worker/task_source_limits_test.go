package worker

import (
	"reflect"
	"strings"
	"testing"
)

func resourceFixtureTask(t *testing.T, files map[string]string) Task {
	for path, source := range files {
		if source == "" {
			t.Fatalf("fixture source for %q is empty", path)
		}
		for _, r := range source {
			if r < 0x20 || r > 0x7e {
				t.Fatalf("fixture source for %q is not ASCII", path)
			}
		}
	}
	task := Task{
		Brief: "resource fixture",
		Files: files,
	}
	for path, source := range files {
		view, err := NewBoundedFileView(path, source, Hash(source), 0, 1)
		if err != nil {
			t.Fatalf("NewBoundedFileView(%q) = %v", path, err)
		}
		if task.FileViews == nil {
			task.FileViews = make(map[string][]BoundedFileView)
		}
		task.FileViews[path] = append(task.FileViews[path], view)
	}
	return task
}

func testSourceLimit(t *testing.T, name string, files map[string]string, wantErr bool) {
	task := resourceFixtureTask(t, files)
	snapshot, canonical, wire, err := prepareWorkerTask(task)
	if wantErr {
		if err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
		if !reflect.DeepEqual(snapshot, Task{}) {
			t.Fatalf("%s: expected zero Task, got %+v", name, snapshot)
		}
		if canonical != nil {
			t.Fatalf("%s: expected nil canonical, got %d bytes", name, len(canonical))
		}
		if wire != nil {
			t.Fatalf("%s: expected nil wire, got %d bytes", name, len(wire))
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: expected success, got %v", name, err)
	}
	if canonical == nil {
		t.Fatalf("%s: expected canonical, got nil", name)
	}
	if wire == nil {
		t.Fatalf("%s: expected wire, got nil", name)
	}
}

func TestSourceLimit(t *testing.T) {
	twoMiB := strings.Repeat("s", 2*1024*1024)
	twoMiBPlus1 := twoMiB + "s"
	cases := []struct {
		name    string
		files   map[string]string
		wantErr bool
	}{
		{
			name:    "one source at 2MiB passes",
			files:   map[string]string{"a": twoMiB},
			wantErr: false,
		},
		{
			name:    "one source at 2MiB+1 fails",
			files:   map[string]string{"a": twoMiBPlus1},
			wantErr: true,
		},
		{
			name:    "four sources at 2MiB each pass at 8MiB aggregate",
			files:   map[string]string{"a": twoMiB, "b": twoMiB, "c": twoMiB, "d": twoMiB},
			wantErr: false,
		},
		{
			name:    "four sources at 2MiB plus one byte fails at 8MiB+1",
			files:   map[string]string{"a": twoMiB, "b": twoMiB, "c": twoMiB, "d": twoMiB, "e": "s"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		testSourceLimit(t, c.name, c.files, c.wantErr)
	}
}
