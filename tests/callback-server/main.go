package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

var (
	successCount int64
	slowCount    int64
	errorCount   int64
)

type CallbackResult struct {
	Endpoint  string `json:"endpoint"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Count     int64  `json:"count"`
}

func successHandler(w http.ResponseWriter, r *http.Request) {
	count := atomic.AddInt64(&successCount, 1)
	log.Printf("[SUCCESS] #%d - %s %s", count, r.Method, r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CallbackResult{
		Endpoint:  "/callback/success",
		Message:   "任务执行成功",
		Timestamp: time.Now().Format(time.RFC3339),
		Count:     count,
	})
}

func slowHandler(w http.ResponseWriter, r *http.Request) {
	count := atomic.AddInt64(&slowCount, 1)
	log.Printf("[SLOW] #%d - 开始处理，预计耗时 10 秒...", count)

	time.Sleep(10 * time.Second)

	log.Printf("[SLOW] #%d - 处理完成", count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CallbackResult{
		Endpoint:  "/callback/slow",
		Message:   "慢任务执行完成（耗时 10 秒）",
		Timestamp: time.Now().Format(time.RFC3339),
		Count:     count,
	})
}

func errorHandler(w http.ResponseWriter, r *http.Request) {
	count := atomic.AddInt64(&errorCount, 1)
	log.Printf("[ERROR] #%d - %s %s", count, r.Method, r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(CallbackResult{
		Endpoint:  "/callback/error",
		Message:   "任务执行失败（模拟服务端错误）",
		Timestamp: time.Now().Format(time.RFC3339),
		Count:     count,
	})
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{
		"success_total": atomic.LoadInt64(&successCount),
		"slow_total":    atomic.LoadInt64(&slowCount),
		"error_total":   atomic.LoadInt64(&errorCount),
	})
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/callback/success", successHandler)
	mux.HandleFunc("/callback/slow", slowHandler)
	mux.HandleFunc("/callback/error", errorHandler)
	mux.HandleFunc("/stats", statsHandler)

	addr := ":9091"
	fmt.Println("=== ChronoFlow 测试回调服务 ===")
	fmt.Printf("监听地址: http://localhost%s\n", addr)
	fmt.Println()
	fmt.Println("可用接口:")
	fmt.Printf("  POST http://localhost%s/callback/success  - 立即成功\n", addr)
	fmt.Printf("  POST http://localhost%s/callback/slow     - 延迟 10 秒后成功（测试超时）\n", addr)
	fmt.Printf("  POST http://localhost%s/callback/error    - 返回 500 错误（测试重试）\n", addr)
	fmt.Printf("  GET  http://localhost%s/stats             - 查看调用统计\n", addr)
	fmt.Println()

	log.Fatal(http.ListenAndServe(addr, mux))
}
