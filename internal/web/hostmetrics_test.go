package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestParseNvidiaMetricsKeepsSeparateGPUs(t *testing.T) {
	rows := parseNvidiaMetrics("0, NVIDIA A, 12, 1024, 16384\n1, NVIDIA B, 80, 2048, 8192\n")
	if len(rows) != 2 || rows[0].Index != 0 || rows[1].Index != 1 ||
		rows[0].UtilizationPercent == nil || *rows[0].UtilizationPercent != 12 ||
		rows[1].VRAMUsedBytes != 2048*1024*1024 || rows[1].VRAMTotalBytes != 8192*1024*1024 {
		t.Fatalf("per-GPU metrics lost: %+v", rows)
	}
}

func TestSystemMetricsEndpointReturnsMeasuredOrUnavailable(t *testing.T) {
	server := testHTTPServer(t)
	response, err := http.Get(server.URL + "/api/system/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metrics status: %d", response.StatusCode)
	}
	var metrics hostMetrics
	if err := json.NewDecoder(response.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.CapturedAt.IsZero() || (metrics.RAMTotalBytes > 0 && metrics.RAMUsedBytes > metrics.RAMTotalBytes) {
		t.Fatalf("invalid metrics: %+v", metrics)
	}
}
