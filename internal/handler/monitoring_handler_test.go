package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestQueryRangeParsesPrometheusMatrixAndSkipsNaN 验证对应的测试场景
func TestQueryRangeParsesPrometheusMatrixAndSkipsNaN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "up" {
			t.Fatalf("query = %q, want up", got)
		}
		if got := r.URL.Query().Get("step"); got != "30" {
			t.Fatalf("step = %q, want 30", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"values":[[1710000000,"1"],[1710000030,"NaN"],[1710000060,"0"]]}]}}`))
	}))
	defer server.Close()

	handler := &MonitoringHandler{promURL: server.URL, client: server.Client()}
	points, err := handler.queryRange(context.Background(), "up", time.Now().Add(-time.Hour), time.Now(), 30)
	if err != nil {
		t.Fatalf("queryRange returned error: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("points length = %d, want 2", len(points))
	}
	if points[0].Timestamp != 1710000000 || points[0].Value != 1 || points[1].Value != 0 {
		t.Fatalf("points = %#v, want parsed finite values", points)
	}
}

// TestHistoryWindowAllowsSupportedRangesOnly 验证对应的测试场景
func TestHistoryWindowAllowsSupportedRangesOnly(t *testing.T) {
	tests := []struct {
		raw     string
		minutes int
		step    int
	}{
		{raw: "15", minutes: 15, step: 10},
		{raw: "360", minutes: 360, step: 120},
		{raw: "1440", minutes: 1440, step: 300},
		{raw: "999", minutes: 60, step: 30},
	}

	for _, tt := range tests {
		minutes, step := historyWindow(tt.raw)
		if minutes != tt.minutes || step != tt.step {
			t.Errorf("historyWindow(%q) = (%d, %d), want (%d, %d)", tt.raw, minutes, step, tt.minutes, tt.step)
		}
	}
}
