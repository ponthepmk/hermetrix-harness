package web

import (
	"context"
	"encoding/csv"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type hostGPU struct {
	Index              int      `json:"index"`
	Name               string   `json:"name"`
	UtilizationPercent *float64 `json:"utilization_percent"`
	VRAMUsedBytes      uint64   `json:"vram_used_bytes"`
	VRAMTotalBytes     uint64   `json:"vram_total_bytes"`
}

type hostMetrics struct {
	CapturedAt    time.Time `json:"captured_at"`
	CPUPercent    *float64  `json:"cpu_percent"`
	RAMUsedBytes  uint64    `json:"ram_used_bytes"`
	RAMTotalBytes uint64    `json:"ram_total_bytes"`
	GPUs          []hostGPU `json:"gpus"`
	GPUStatus     string    `json:"gpu_status"`
}

func (s *Server) systemMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	metrics := hostMetrics{CapturedAt: time.Now(), GPUs: []hostGPU{}, GPUStatus: "unavailable"}
	idleBefore, totalBefore, cpuOK := readCPUTicks()
	used, total, memoryOK := readSystemMemory()
	if memoryOK {
		metrics.RAMUsedBytes, metrics.RAMTotalBytes = used, total
	}
	command := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=index,name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits")
	if output, err := command.Output(); err == nil {
		metrics.GPUs = parseNvidiaMetrics(string(output))
		if len(metrics.GPUs) > 0 {
			metrics.GPUStatus = "ready"
		}
	}
	if cpuOK {
		select {
		case <-time.After(120 * time.Millisecond):
		case <-ctx.Done():
		}
		idleAfter, totalAfter, ok := readCPUTicks()
		if ok && totalAfter > totalBefore && idleAfter >= idleBefore && idleAfter-idleBefore <= totalAfter-totalBefore {
			busy := float64(totalAfter-totalBefore-(idleAfter-idleBefore)) / float64(totalAfter-totalBefore) * 100
			if busy < 0 {
				busy = 0
			} else if busy > 100 {
				busy = 100
			}
			metrics.CPUPercent = &busy
		}
	}
	writeJSON(w, http.StatusOK, metrics)
}

func parseNvidiaMetrics(output string) []hostGPU {
	reader := csv.NewReader(strings.NewReader(output))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil
	}
	gpus := make([]hostGPU, 0, len(records))
	for _, record := range records {
		if len(record) != 5 {
			continue
		}
		index, indexErr := strconv.Atoi(strings.TrimSpace(record[0]))
		used, usedErr := strconv.ParseUint(strings.TrimSpace(record[3]), 10, 64)
		total, totalErr := strconv.ParseUint(strings.TrimSpace(record[4]), 10, 64)
		if indexErr != nil || usedErr != nil || totalErr != nil || total == 0 {
			continue
		}
		gpu := hostGPU{Index: index, Name: strings.TrimSpace(record[1]), VRAMUsedBytes: used * 1024 * 1024, VRAMTotalBytes: total * 1024 * 1024}
		if percent, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64); err == nil && percent >= 0 && percent <= 100 {
			gpu.UtilizationPercent = &percent
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}
