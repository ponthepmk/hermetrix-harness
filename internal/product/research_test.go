package product

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func TestResearchDiscoveryPreservesMetadataAndTaskEgress(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	tasks := taskengine.NewService(dataStore)
	create := func(policy string) string {
		input := taskengine.CreateTaskInput{Title: "Article research", Objective: "Find current sources", OriginalRequest: "Find current sources", Actor: "owner",
			Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Sources can be reviewed"}}, EgressPolicy: policy}
		if policy == "remote_allowed" {
			input.RemoteEgressApproval = &taskengine.TaskEgressApproval{Actor: "owner", Reason: "Search Crossref"}
		}
		item, err := tasks.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	localTask := create("local_only")
	remoteTask := create("remote_allowed")
	var calls int
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("query.bibliographic"); got != "AI small business" {
			t.Errorf("query=%q", got)
		}
		if got := r.URL.Query().Get("rows"); got != "10" {
			t.Errorf("rows=%q", got)
		}
		if !strings.Contains(r.URL.Query().Get("filter"), "type:journal-article") {
			t.Errorf("filter=%q", r.URL.Query().Get("filter"))
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"message":{"items":[
			{"DOI":"10.1234/ABC","title":["Useful finding"],"publisher":"Example Press","type":"journal-article","published":{"date-parts":[[2026,9,25]]}},
			{"DOI":"10.1234/DEF","title":["Only month known"],"publisher":"Example Press","type":"journal-article","published":{"date-parts":[[2026,9]]}},
			{"DOI":"","title":["No stable source"]}
		]}}`
		if calls >= 3 {
			body = strings.Replace(body, "Useful finding", "Corrected title", 1)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer fixture.Close()
	service.researchClient = fixture.Client()
	service.researchEndpoint = fixture.URL
	if _, err := service.SearchResearchArticles(ctx, localTask, "AI small business", 30); !errors.Is(err, ErrResearchEgressBlocked) {
		t.Fatalf("local-only search err=%v", err)
	}
	if calls != 0 {
		t.Fatal("local-only task sent query externally")
	}
	items, err := service.SearchResearchArticles(ctx, remoteTask, "AI small business", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].PublishedDate != "2026-09-25" || items[0].PublishedPrecision != "day" || items[1].PublishedDate != "2026-09" || items[1].PublishedPrecision != "month" {
		t.Fatalf("metadata=%+v", items)
	}
	if !strings.HasPrefix(items[0].SourceURL, "https://doi.org/") || len(items[0].SourceDigest) != 64 || items[0].RetrievedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("provenance=%+v", items[0])
	}
	selected, err := service.SelectResearchArticle(ctx, remoteTask, items[0].ID, true)
	if err != nil || !selected.Selected {
		t.Fatalf("selection=%+v err=%v", selected, err)
	}
	if _, err := service.SelectResearchArticle(ctx, localTask, items[0].ID, false); !errors.Is(err, ErrResearchNotFound) {
		t.Fatalf("cross-task selection err=%v", err)
	}
	items, err = service.SearchResearchArticles(ctx, remoteTask, "AI small business", 30)
	if err != nil || len(items) != 2 || !items[0].Selected || items[0].ID != selected.ID {
		t.Fatalf("repeat results=%+v err=%v", items, err)
	}
	items, err = service.SearchResearchArticles(ctx, remoteTask, "AI small business", 30)
	if err != nil || len(items) != 2 || items[0].Selected || items[0].Title != "Corrected title" {
		t.Fatalf("changed source did not clear selection: %+v err=%v", items, err)
	}
}

func TestCrossrefDateDoesNotInventPrecision(t *testing.T) {
	for _, test := range []struct {
		parts           []int
		date, precision string
	}{
		{[]int{2026}, "2026", "year"}, {[]int{2026, 9}, "2026-09", "month"},
		{[]int{2026, 9, 25}, "2026-09-25", "day"}, {[]int{2026, 2, 30}, "", ""},
	} {
		date, precision := formatCrossrefDate(crossrefDate{DateParts: [][]int{test.parts}})
		if date != test.date || precision != test.precision {
			t.Fatalf("parts=%v got %q/%q", test.parts, date, precision)
		}
	}
}
