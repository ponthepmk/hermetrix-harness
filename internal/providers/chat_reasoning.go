package providers

import "context"

// Reuse the actual adapter's verified local /props probe, rather than assuming
// that any OpenAI-compatible endpoint understands llama.cpp extensions.
func (s *Service) SupportsChatReasoningBudget(ctx context.Context, profile Profile) bool {
	adapter, ok := s.adapters[profile.AdapterKind].(*OpenAIAdapter)
	if !ok || !IsLocalProfile(profile) {
		return false
	}
	key, err := s.credential(profile)
	if err != nil {
		return false
	}
	return adapter.supportsLocalReasoningBudget(ctx, profile, key)
}
