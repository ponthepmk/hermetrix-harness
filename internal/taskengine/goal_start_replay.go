package taskengine

// replayGoalStart validates that the caller's requested SHA-256 matches the
// canonical digest for the original (incoming) request revision.
//
// It requires:
//   - g.Config.StartInputSHA256 is nonempty and equals requestedSHA
//   - recomputing goalStartInputSHA256 from g.TaskID, originalExpectedRevision,
//     g.Actor and g.Config yields requestedSHA
//
// On any mismatch it returns a zero GoalRun and ErrStaleRevision. Hash errors
// return a zero GoalRun and the error. On success it returns g unchanged.
//
// It does not compare g.ExpectedTaskRevision, does not mutate g, does not
// dispatch, does not query the DB, and does not trust the caller's marker.
func replayGoalStart(g GoalRun, requestedSHA string, originalExpectedRevision int) (GoalRun, error) {
	if g.Config.StartInputSHA256 == "" || g.Config.StartInputSHA256 != requestedSHA {
		return GoalRun{}, ErrStaleRevision
	}
	hash, err := goalStartInputSHA256(g.TaskID, originalExpectedRevision, g.Actor, g.Config)
	if err != nil {
		return GoalRun{}, err
	}
	if hash != requestedSHA {
		return GoalRun{}, ErrStaleRevision
	}
	return g, nil
}
