package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/gin-gonic/gin"
)

func TestOperationalHealthIncludesRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, role := range allRoles {
		t.Run(string(role), func(t *testing.T) {
			router := gin.New()
			registerOperationalRoutes(
				router,
				role,
				role.Components(),
				metrics.NewReporter(),
				func(context.Context) map[string]string { return nil },
			)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("GET /health status = %d, want %d", recorder.Code, http.StatusOK)
			}

			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["role"] != string(role) {
				t.Fatalf("role = %v, want %q", body["role"], role)
			}
			if body["runtime_mode"] != role.RuntimeMode() {
				t.Fatalf("runtime_mode = %v, want %q", body["runtime_mode"], role.RuntimeMode())
			}
		})
	}
}

func TestOperationalReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       Role
		failures   map[string]string
		wantStatus int
		wantReady  string
	}{
		{name: "ready", role: RoleAPI, wantStatus: http.StatusOK, wantReady: "ready"},
		{
			name:       "dependency failure",
			role:       RoleAPI,
			failures:   map[string]string{"redis": "connection refused"},
			wantStatus: http.StatusServiceUnavailable,
			wantReady:  "not_ready",
		},
		{name: "worker ready", role: RoleWorker, wantStatus: http.StatusOK, wantReady: "ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			registerOperationalRoutes(
				router,
				tt.role,
				tt.role.Components(),
				metrics.NewReporter(),
				func(context.Context) map[string]string { return tt.failures },
			)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/ready", nil)
			router.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("GET /ready status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["status"] != tt.wantReady {
				t.Fatalf("status = %v, want %q", body["status"], tt.wantReady)
			}
		})
	}
}
