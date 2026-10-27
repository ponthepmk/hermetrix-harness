package providers

// EstimateRequestTokens returns the same token estimate used by Service.StreamChat
// for prospective host budget fitting.
func EstimateRequestTokens(request ChatRequest) int {
	return estimateRequestTokens(request)
}
