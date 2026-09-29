//go:build !windows && !linux

package web

func readCPUTicks() (idle, total uint64, ok bool)     { return 0, 0, false }
func readSystemMemory() (used, total uint64, ok bool) { return 0, 0, false }
