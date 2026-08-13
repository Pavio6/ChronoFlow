//go:build e2e

// 真实角色进程的端到端场景：通过 API 驱动，不直接操作业务表

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type timerResponse struct {
	Code int `json:"code"`
	Data struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

// executionResponse 只保留当前断言需要的 Execution 字段
type executionResponse struct {
	Code int `json:"code"`
	Data []struct {
		ID      int64  `json:"id"`
		Status  string `json:"status"`
		Attempt int    `json:"attempt"`
	} `json:"data"`
}

// TestE2E_SchedulesAndExecutesCallback 验证 Timer 从 API 创建、调度、投递到 HTTP 回调成功的完整链路
func TestE2E_SchedulesAndExecutesCallback(t *testing.T) {
	logE2ERuntime(t)
	// 本地 HTTP 服务扮演外部业务回调端点，成功时返回 204
	callbackCalls := make(chan struct{}, 4)
	callback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case callbackCalls <- struct{}{}:
		default:
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()

	// API 创建并激活后，后续 Scheduler、Dispatcher、Worker 均由真实进程完成
	timerID := createAndActivateTimer(t, callback.URL)
	defer deactivateTimer(t, timerID)

	select {
	case <-callbackCalls:
	case <-time.After(15 * time.Second):
		t.Fatalf("callback was not invoked within timeout\nrole logs:%s", roleLogs())
	}

	// 回调收到不代表最终状态已落库，继续确认 Execution 已成功持久化
	eventually(t, 10*time.Second, func() (bool, error) {
		executions, err := timerExecutions(timerID)
		if err != nil {
			return false, err
		}
		for _, execution := range executions.Data {
			if execution.Status == "SUCCESS" && execution.Attempt == 1 {
				return true, nil
			}
		}
		return false, nil
	})
}

// TestE2E_RetriesRetryableCallbackFailure 验证可重试回调失败会经 Outbox 再次投递并最终成功
func TestE2E_RetriesRetryableCallbackFailure(t *testing.T) {
	logE2ERuntime(t)
	// 第一次返回 500（可重试），第二次开始返回 204，验证重试 Outbox 的完整再投递链路
	var callbackAttempts atomic.Int32
	callback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if callbackAttempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()

	timerID := createAndActivateTimer(t, callback.URL)
	defer deactivateTimer(t, timerID)

	// 必须确认同一条 Execution 至少经历两次尝试后成功，而非后续 Cron 产生的新 Execution 成功
	eventually(t, 15*time.Second, func() (bool, error) {
		executions, err := timerExecutions(timerID)
		if err != nil {
			return false, err
		}
		for _, execution := range executions.Data {
			if execution.Status == "SUCCESS" && execution.Attempt >= 2 {
				return callbackAttempts.Load() >= 2, nil
			}
		}
		return false, nil
	})
}

func createAndActivateTimer(t *testing.T, callbackURL string) int64 {
	t.Helper()
	// 使用每秒触发的六字段 Cron，让场景在有限时间内产生第一条 Execution
	body, err := json.Marshal(map[string]any{
		"app":             "e2e",
		"name":            fmt.Sprintf("e2e-%d", time.Now().UnixNano()),
		"cron_expr":       "* * * * * *",
		"callback_url":    callbackURL,
		"callback_method": "POST",
		"misfire_policy":  "FIRE_ONCE",
	})
	if err != nil {
		t.Fatalf("marshal timer request: %v", err)
	}

	response, err := http.Post(apiBaseURL()+"/api/v1/timers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create timer: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create timer returned status %d", response.StatusCode)
	}
	var payload timerResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create timer response: %v", err)
	}
	if payload.Data.ID < 1 {
		t.Fatalf("create timer returned invalid id %d", payload.Data.ID)
	}

	// 创建后的 Timer 默认为 INACTIVE，显式激活才会写入 next_fire_at 并进入 Scheduler 扫描范围
	activateResponse, err := http.Post(fmt.Sprintf("%s/api/v1/timers/%d/activate", apiBaseURL(), payload.Data.ID), "application/json", nil)
	if err != nil {
		t.Fatalf("activate timer: %v", err)
	}
	defer activateResponse.Body.Close()
	if activateResponse.StatusCode != http.StatusOK {
		t.Fatalf("activate timer returned status %d", activateResponse.StatusCode)
	}
	return payload.Data.ID
}

func deactivateTimer(t *testing.T, timerID int64) {
	t.Helper()
	// 场景结束后停止每秒触发的 Timer，防止它影响同一套件中的后续场景
	response, err := http.Post(fmt.Sprintf("%s/api/v1/timers/%d/deactivate", apiBaseURL(), timerID), "application/json", nil)
	if err != nil {
		t.Logf("deactivate timer %d: %v", timerID, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Logf("deactivate timer %d returned status %d", timerID, response.StatusCode)
	}
}

func timerExecutions(timerID int64) (executionResponse, error) {
	// 通过 API 查询而非直接查库，验证管理面能够观察到执行最终状态
	response, err := http.Get(fmt.Sprintf("%s/api/v1/timers/%d/executions?limit=100", apiBaseURL(), timerID))
	if err != nil {
		return executionResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return executionResponse{}, fmt.Errorf("list executions returned status %d", response.StatusCode)
	}
	var payload executionResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return executionResponse{}, err
	}
	return payload, nil
}
