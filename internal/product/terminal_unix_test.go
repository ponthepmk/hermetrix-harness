//go:build !windows

package product

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestTerminalPersistenceAppendsOnlyTheChunkAndKeepsABoundedByteTail(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "terminal append", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO terminal_sessions
		(id,project_id,shell,working_dir,state,output_tail,cursor,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"term_append", project.ID, "sh", ".", "running", "", 0, formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	first := bytes.Repeat([]byte("a"), 700<<10)
	second := bytes.Repeat([]byte("b"), 700<<10)
	if err := service.appendTerminalOutput(ctx, "term_append", first, int64(len(first)), now); err != nil {
		t.Fatal(err)
	}
	if err := service.appendTerminalOutput(ctx, "term_append", second, int64(len(first)+len(second)), now); err != nil {
		t.Fatal(err)
	}
	var tail []byte
	var cursor int64
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT output_tail,cursor FROM terminal_sessions WHERE id='term_append'`).Scan(&tail, &cursor); err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte(nil), first...), second...)
	want = want[len(want)-terminalTailLimit:]
	if !bytes.Equal(tail, want) {
		t.Fatalf("persisted tail has %d bytes and wrong suffix; want %d", len(tail), len(want))
	}
	if cursor != int64(len(first)+len(second)) {
		t.Fatalf("cursor = %d, want %d", cursor, len(first)+len(second))
	}
}

func TestTerminalPersistenceRefusesToAppendAfterTheSessionStops(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	now := formatTime(time.Now().UTC())
	project, err := service.SaveProject(ctx, ProjectInput{Name: "terminal stopped", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO terminal_sessions
		(id,project_id,shell,working_dir,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"term_stopped", project.ID, "sh", ".", "completed", now, now); err != nil {
		t.Fatal(err)
	}
	if err := service.appendTerminalOutput(ctx, "term_stopped", []byte("late"), 4, time.Now().UTC()); err == nil {
		t.Fatal("output was appended after the terminal stopped")
	}
}
