package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAPISecurityRequiresConfiguredKeyOnlyForAPI 验证对应的测试场景。
func TestAPISecurityRequiresConfiguredKeyOnlyForAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(APISecurity("secret", 1024))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/v1/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, test := range []struct {
		path       string
		key        string
		wantStatus int
	}{
		{path: "/health", wantStatus: http.StatusOK},
		{path: "/api/v1/test", wantStatus: http.StatusUnauthorized},
		{path: "/api/v1/test", key: "secret", wantStatus: http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.key != "" {
			request.Header.Set("X-API-Key", test.key)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.wantStatus {
			t.Errorf("%s status = %d, want %d", test.path, recorder.Code, test.wantStatus)
		}
	}
}

// TestCORSReflectsOnlyAllowedOrigin 验证对应的测试场景。
func TestCORSReflectsOnlyAllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS([]string{"https://console.example.com"}))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, test := range []struct {
		origin string
		want   string
	}{
		{origin: "https://console.example.com", want: "https://console.example.com"},
		{origin: "https://evil.example.com", want: ""},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Origin", test.origin)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != test.want {
			t.Errorf("origin %q response = %q, want %q", test.origin, got, test.want)
		}
	}
}
