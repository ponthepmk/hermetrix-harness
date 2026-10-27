//go:build !windows

package product

func retryableBrowserProfileError(error) bool { return false }
