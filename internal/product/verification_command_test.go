package product

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDurableVerificationCapacityIsAtomicAndDoesNotRelaxInteractiveTimeout(t *testing.T) {
	s, _, _ := testProductService(t)
	ctx := context.Background()
	p, err := s.SaveProject(ctx, ProjectInput{Name: "capacity", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.ProjectRootIdentity(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	template := CommandInput{ProjectID: p.ID, ExpectedRoot: root, Actor: "owner", Executable: "node", Arguments: []string{"-e", "setInterval(()=>{},1000)"}, WorkingDir: ".", TimeoutSeconds: 900}
	if _, err = s.StartCommand(ctx, template); err == nil {
		t.Fatal("interactive command inherited long verification timeout")
	}
	if _, err = s.StartVerificationCommand(ctx, template); err == nil {
		t.Fatal("long command accepted without durable operation identity")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := template
			input.OperationID = fmt.Sprintf("capacity-operation-%d", i)
			_, err := s.StartVerificationCommand(ctx, input)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrVerificationBusy) {
			t.Fatal(err)
		}
	}
	if wins != maxVerificationCommands {
		t.Fatalf("concurrent publications=%d", wins)
	}
	available, err := s.VerificationCapacity(ctx)
	if err != nil || available {
		t.Fatalf("capacity=%v err=%v", available, err)
	}
	jobs, err := s.ListJobs(ctx, 20)
	if err != nil || len(jobs) != maxVerificationCommands {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	for _, job := range jobs {
		if _, err = s.CancelJob(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		available, err = s.VerificationCapacity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if available {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("capacity not released after command cancellation")
}
