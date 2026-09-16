package context

import "fmt"

type Profile struct {
	Name               string `json:"name"`
	Total              int    `json:"total"`
	OutputReserve      int    `json:"output_reserve"`
	UncertaintyReserve int    `json:"uncertainty_reserve"`
	SystemBudget       int    `json:"system_budget"`
	DirectToolBudget   int    `json:"direct_tool_budget"`
	SkillProjectBudget int    `json:"skill_project_budget"`
	PinnedBudget       int    `json:"pinned_budget"`
	ActiveBudget       int    `json:"active_budget"`
	SummaryTarget      int    `json:"summary_target"`
	MaxInlineTool      int    `json:"max_inline_tool"`
}

func Certified64K() Profile {
	return Profile{Name: "certified-64k", Total: 65536, OutputReserve: 8192,
		UncertaintyReserve: 4096, SystemBudget: 3072, DirectToolBudget: 4096,
		SkillProjectBudget: 6144, PinnedBudget: 3072, ActiveBudget: 36864,
		SummaryTarget: 2048, MaxInlineTool: 2000}
}

// Extended96K is the primary constrained-local envelope. 96 Ki tokens means
// 96 * 1024 = 98,304, matching the binary capacities used by the other
// profiles. It leaves enough active space for repository evidence while still
// reserving one eighth of the window for the answer.
func Extended96K() Profile {
	return Profile{Name: "extended-96k", Total: 98304, OutputReserve: 12288,
		UncertaintyReserve: 6144, SystemBudget: 3584, DirectToolBudget: 4096,
		SkillProjectBudget: 9216, PinnedBudget: 4608, ActiveBudget: 58368,
		SummaryTarget: 3072, MaxInlineTool: 3000}
}

func Extended128K() Profile {
	return Profile{Name: "extended-128k", Total: 131072, OutputReserve: 16384,
		UncertaintyReserve: 8192, SystemBudget: 4096, DirectToolBudget: 4096,
		SkillProjectBudget: 12288, PinnedBudget: 6144, ActiveBudget: 79872,
		SummaryTarget: 4096, MaxInlineTool: 4000}
}

func Extended256K() Profile {
	return Profile{Name: "extended-256k", Total: 262144, OutputReserve: 32768,
		UncertaintyReserve: 16384, SystemBudget: 6144, DirectToolBudget: 6144,
		SkillProjectBudget: 24576, PinnedBudget: 12288, ActiveBudget: 163840,
		SummaryTarget: 8192, MaxInlineTool: 8000}
}

func Ultra1M() Profile {
	return Profile{Name: "ultra-1m", Total: 1048576, OutputReserve: 65536,
		UncertaintyReserve: 32768, SystemBudget: 8192, DirectToolBudget: 8192,
		SkillProjectBudget: 65536, PinnedBudget: 32768, ActiveBudget: 835584,
		SummaryTarget: 32768, MaxInlineTool: 16000}
}

func Compact32K() Profile {
	return Profile{Name: "compact-32k", Total: 32768, OutputReserve: 4096,
		UncertaintyReserve: 2048, SystemBudget: 2048, DirectToolBudget: 3584,
		SkillProjectBudget: 3072, PinnedBudget: 2048, ActiveBudget: 15872,
		SummaryTarget: 1280, MaxInlineTool: 1000}
}

func Profiles() []Profile {
	return []Profile{Compact32K(), Certified64K(), Extended96K(), Extended128K(), Extended256K(), Ultra1M()}
}

func ProfileByName(name string) (Profile, bool) {
	for _, profile := range Profiles() {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

// BestProfileForCapacity returns the largest selectable envelope that fits in
// an observed allocation. Qualification uses this same registry as the
// compiler and UI, so a profile cannot exist in one subsystem and disappear
// in another.
func BestProfileForCapacity(capacity int) (Profile, bool) {
	var best Profile
	for _, profile := range Profiles() {
		if profile.Total <= capacity && profile.Total > best.Total {
			best = profile
		}
	}
	return best, best.Total > 0
}

func (p Profile) Validate() error {
	if p.Total <= 0 || p.OutputReserve < 0 || p.UncertaintyReserve < 0 {
		return fmt.Errorf("invalid context profile totals")
	}
	sum := p.OutputReserve + p.UncertaintyReserve + p.SystemBudget + p.DirectToolBudget + p.SkillProjectBudget + p.PinnedBudget + p.ActiveBudget
	if sum != p.Total {
		return fmt.Errorf("profile %s slices total %d, want %d", p.Name, sum, p.Total)
	}
	if p.SummaryTarget < 0 || p.SummaryTarget > p.ActiveBudget/2 {
		return fmt.Errorf("profile %s has invalid summary target", p.Name)
	}
	return nil
}
