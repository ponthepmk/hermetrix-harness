package taskcoord

func proposalInitialViewLimit(contextMax int) int {
	if contextMax >= 24576 {
		return 96 * 1024
	}
	limit := contextMax * 4
	if limit < 256 {
		return 256
	}
	return limit
}
