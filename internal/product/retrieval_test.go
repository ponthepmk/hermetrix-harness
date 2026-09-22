package product

import (
	"context"
	"testing"
)

func TestRetrieveMemoriesRanksActiveProjectKnowledge(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Retrieval", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "debug", Content: "SQLite busy timeout fixes concurrent writer failures", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "style", Content: "Use blue buttons", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	matches, err := service.RetrieveMemories(ctx, project.ID, "fix SQLite writer timeout failure", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].MemoryKind != "debug" || matches[0].Score < 2 {
		t.Fatalf("matches=%+v", matches)
	}
}
