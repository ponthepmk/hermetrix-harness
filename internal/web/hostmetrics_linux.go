package web

import (
	"os"
	"strconv"
	"strings"
)

func readCPUTicks() (idle, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for index, raw := range fields[1:] {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += value
		if index == 3 || index == 4 {
			idle += value
		}
	}
	return idle, total, true
}

func readSystemMemory() (used, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var available uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value * 1024
		case "MemAvailable:":
			available = value * 1024
		}
	}
	if total == 0 || available > total {
		return 0, 0, false
	}
	return total - available, total, true
}
