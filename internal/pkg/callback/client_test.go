package callback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chronoflow/internal/model"
)

func TestValidateURLRejectsPrivateTargets(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1/callback",
		"http://[::1]/callback",
		"http://localhost/callback",
		"file:///etc/passwd",
		"http://user:secret@example.com/callback",
	} {
		if err := ValidateURL(target, false); err == nil {
			t.Errorf("ValidateURL(%q) returned nil error", target)
		}
	}
	if err := ValidateURL("https://example.com/callback", false); err != nil {
		t.Fatalf("public URL rejected: %v", err)
	}
}

func TestClientSendsStableIdempotencyKey(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		gotKey = request.Header.Get("Idempotency-Key")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(time.Second, 1024, true)
	code, _, err := client.Execute(
		context.Background(),
		&model.CallbackSnapshot{
			URL:    server.URL,
			Method: http.MethodPost,
			Headers: map[string]string{
				"Idempotency-Key": "caller-controlled",
			},
		},
		42,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code != http.StatusNoContent {
		t.Fatalf("code = %d, want %d", code, http.StatusNoContent)
	}
	if gotKey != "chronoflow-execution-42" {
		t.Fatalf("Idempotency-Key = %q", gotKey)
	}
}

func TestClientLimitsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = writer.Write([]byte("response-too-large"))
	}))
	defer server.Close()

	client := NewClient(time.Second, 8, true)
	_, body, err := client.Execute(
		context.Background(),
		&model.CallbackSnapshot{URL: server.URL, Method: http.MethodGet},
		1,
	)
	if err == nil {
		t.Fatal("Execute returned nil error for oversized response")
	}
	if len(body) != 8 {
		t.Fatalf("body length = %d, want 8", len(body))
	}
}
