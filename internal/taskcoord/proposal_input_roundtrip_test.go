package taskcoord

import (
	"context"
	"reflect"
	"testing"
)

func TestLoadProposalInputRoundtrip(t *testing.T) {
	f := newProposalInputFixture(t)
	got, err := f.Coordinator.loadProposalInput(context.Background(), f.Proposal, f.Outer, f.Bundle)
	if err != nil {
		t.Fatalf("loadProposalInput: %v", err)
	}
	if !reflect.DeepEqual(got, f.Task) {
		t.Fatalf("loaded task differs from fixture task")
	}
	original := got.FileViews["sum.go"][0].Content
	got.FileViews["sum.go"][0].Content = original + "mutated"
	if got.FileViews["sum.go"][0].Content != original+"mutated" {
		t.Fatalf("mutated view not reflected")
	}
	if f.Task.FileViews["sum.go"][0].Content != original {
		t.Fatalf("fixture task view mutated")
	}
}
