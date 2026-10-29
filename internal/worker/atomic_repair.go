package worker

import "errors"

// atomicRepair is an alternative submission form, not additional write scope.
// The host supplies all unchanged missing companions from frozen provenance.
type atomicRepair struct {
	Path     string  `json:"path"`
	TargetID string  `json:"target_id"`
	New      *string `json:"new"`
}

func materializeAtomicRepair(task Task, repair atomicRepair) ([]Change, error) {
	if task.RepairTargetVersion != AtomicRepairTargetVersion || repair.New == nil || !writablePathAllowed(task, repair.Path) {
		return nil, errors.New("atomic repair requires recipe 2 and a selected writable draft")
	}
	change, err := materializeRepairTargetEdits(task, repair.Path, []DraftTargetEdit{{TargetID: repair.TargetID, New: *repair.New}})
	if err != nil {
		return nil, err
	}
	changes := []Change{change}
	for _, path := range requiredRepairPaths(task) {
		if path == repair.Path {
			continue
		}
		retain := true
		companion, err := materializeRepairDraft(task, path, nil, nil, &retain)
		if err != nil {
			return nil, err
		}
		changes = append(changes, companion)
	}
	return changes, nil
}
