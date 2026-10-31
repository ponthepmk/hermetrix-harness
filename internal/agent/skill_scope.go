package agent

import (
	"context"

	"hermetrix-harness/internal/skills"
)

func skillAppliesToProject(item skills.Skill, projectID string) bool {
	return skills.AppliesToProject(item, projectID)
}

func (s *Service) scopedSkillContract(ctx context.Context, contract SessionContract) (SessionContract, error) {
	if s.skills == nil || (len(contract.SkillCatalog) == 0 && len(contract.SelectedSkills) == 0) {
		return contract, nil
	}
	items, err := s.skills.ListSkills(ctx, true)
	if err != nil {
		return contract, err
	}
	allowed := map[string]bool{}
	for _, item := range items {
		allowed[item.ID] = skillAppliesToProject(item, contract.ProjectID)
	}
	filter := func(bindings []SessionSkillBinding) []SessionSkillBinding {
		result := make([]SessionSkillBinding, 0, len(bindings))
		for _, binding := range bindings {
			if allowed[binding.SkillID] {
				result = append(result, binding)
			}
		}
		return result
	}
	contract.SkillCatalog = filter(contract.SkillCatalog)
	contract.SelectedSkills = filter(contract.SelectedSkills)
	return contract, nil
}
