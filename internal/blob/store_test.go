package blob

import (
	"errors"
	"testing"
)

func TestPutIsContentAddressedAndIntegrityChecked(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put([]byte("durable skill snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put([]byte("durable skill snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same content produced %q and %q", first, second)
	}
	got, err := store.Get(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "durable skill snapshot" {
		t.Fatalf("unexpected blob %q", got)
	}
	if _, err := store.Get("../escape"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("unsafe ref error = %v", err)
	}
}

func TestListAndQuarantineAreRecoverable(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := store.Put([]byte("recoverable"))
	items, err := store.List()
	if err != nil || len(items) != 1 || items[0].Ref != ref || items[0].Bytes != int64(len("recoverable")) {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	quarantineRoot := t.TempDir()
	destination, err := store.Quarantine(ref, quarantineRoot)
	if err != nil || destination == "" || store.Exists(ref) {
		t.Fatalf("destination=%q exists=%v err=%v", destination, store.Exists(ref), err)
	}
	if restored, err := store.RestoreFromQuarantine(ref, quarantineRoot); err != nil || restored == "" || !store.Exists(ref) {
		t.Fatalf("restored=%q exists=%v err=%v", restored, store.Exists(ref), err)
	}
	// Repeating recovery is idempotent when the verified active object exists.
	if _, err := store.RestoreFromQuarantine(ref, quarantineRoot); err != nil {
		t.Fatalf("idempotent restore: %v", err)
	}
}
