package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCallbackHandlerTracksDuplicates 验证回调接收器能够识别重复幂等标识
func TestCallbackHandlerTracksDuplicates(t *testing.T) {
	collector := newCollector()
	handler := newHandler(collector, callbackConfig{status: http.StatusNoContent})

	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/callback", nil)
		request.Header.Set("Idempotency-Key", "chronoflow-execution-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("callback status = %d", response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var stats callbackStats
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatalf("decode statistics: %v", err)
	}
	if stats.Total != 2 || stats.Unique != 1 || stats.Duplicates != 1 {
		t.Fatalf("unexpected statistics: %+v", stats)
	}
}

// TestCallbackHandlerReset 验证重置接口能够清空统计
func TestCallbackHandlerReset(t *testing.T) {
	collector := newCollector()
	collector.record("chronoflow-execution-1")
	handler := newHandler(collector, callbackConfig{status: http.StatusNoContent})
	request := httptest.NewRequest(http.MethodPost, "/reset", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d", response.Code)
	}
	if stats := collector.snapshot(); stats.Total != 0 || stats.Unique != 0 {
		t.Fatalf("statistics were not reset: %+v", stats)
	}
}
