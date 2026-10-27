package worker

func reviewCredentialValues(task ReviewTask) []string {
	values := make([]string, 0, 16)
	values = append(values, task.SourceState, task.TaskID, task.ProposalID, task.Objective, task.StepTitle, task.StepInstructions)
	values = append(values, task.StepChecks...)
	values = append(values, task.AcceptanceCriteria...)
	values = append(values, task.Constraints...)
	for path, content := range task.Files {
		values = append(values, path, content)
	}
	for path, file := range task.PartialFiles {
		values = append(values, path, file.BeforeSHA256, file.AfterSHA256)
		for _, region := range file.Regions {
			values = append(values, region.Before.Path, region.Before.FullSHA256, region.Before.Content, region.Before.RegionID, region.AfterContent)
		}
	}
	values = append(values, task.TestEvidence...)
	return values
}
