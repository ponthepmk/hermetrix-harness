package taskengine

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestGoalStartSameIDRace(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	cfg := goalConfig()
	goalID := "goal-1"
	taskID := task.ID

	var wg sync.WaitGroup
	start := make(chan struct{})
	records := make(chan GoalRun, 4)
	errors := make(chan error, 4)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := service.CreateGoalAtID(context.Background(), goalID, taskID, task.Revision, "  owner  ", cfg)
			records <- run
			errors <- err
		}()
	}

	close(start)
	wg.Wait()
	close(records)
	close(errors)

	var firstRun GoalRun
	var firstErr error
	for i := 0; i < 4; i++ {
		if i == 0 {
			firstRun = <-records
			firstErr = <-errors
		} else {
			run := <-records
			err := <-errors
			if err != nil {
				t.Fatalf("race result error: %v", err)
			}
			if !reflect.DeepEqual(firstRun, run) {
				t.Fatalf("records differ: %v vs %v", firstRun, run)
			}
			if !reflect.DeepEqual(firstErr, err) {
				t.Fatalf("errors differ: %v vs %v", firstErr, err)
			}
		}
	}
	if firstErr != nil {
		t.Fatalf("first error: %v", firstErr)
	}

	goals, err := service.GoalsForTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GoalsForTask: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if len(goals[0].Config.StartInputSHA256) != 64 {
		t.Fatalf("marker length: %d", len(goals[0].Config.StartInputSHA256))
	}

	cfg.StartInputSHA256 = "caller-forged-marker"
	replayRun, replayErr := service.CreateGoalAtID(context.Background(), goalID, taskID, task.Revision, "  owner  ", cfg)
	if replayErr != nil {
		t.Fatalf("replay error: %v", replayErr)
	}
	if !reflect.DeepEqual(firstRun, replayRun) {
		t.Fatalf("replay record differs")
	}
}
