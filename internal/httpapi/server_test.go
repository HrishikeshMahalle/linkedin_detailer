package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tradelab/linkedin-profile-api/internal/linkedin"
	"github.com/tradelab/linkedin-profile-api/internal/profile"
	"github.com/tradelab/linkedin-profile-api/internal/service"
)

type profileGetFunc func(context.Context, string, service.Session) (profile.Result, error)

func (f profileGetFunc) Get(ctx context.Context, url string, session service.Session) (profile.Result, error) {
	return f(ctx, url, session)
}

func TestProfileEndpoint(t *testing.T) {
	t.Parallel()
	getter := profileGetFunc(func(_ context.Context, url string, session service.Session) (profile.Result, error) {
		if url != "https://linkedin.com/in/ada-example" {
			t.Errorf("Get() URL = %q", url)
		}
		if session.LIAT != "session-value" || session.JSESSIONID != "ajax:123" {
			t.Errorf("Get() session was not forwarded")
		}
		return profile.Result{
			SchemaVersion: profile.SchemaVersion,
			Profile:       profile.Profile{FullName: "Ada Example"},
			Meta:          profile.Meta{FetchedAt: time.Now(), Warnings: []string{}},
		}, nil
	})
	handler := testHandler(getter, 60, 2)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/profiles", strings.NewReader(`{"url":"https://linkedin.com/in/ada-example","linkedin_session":{"li_at":"session-value","jsession_id":"ajax:123"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "session-value") || strings.Contains(response.Body.String(), "ajax:123") {
		t.Fatal("response leaked LinkedIn session values")
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is missing")
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy header is missing")
	}
	var result profile.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Profile.FullName != "Ada Example" || result.Meta.RequestID == "" {
		t.Errorf("result = %#v", result)
	}
}

func TestProfileEndpointRejectsInvalidBody(t *testing.T) {
	t.Parallel()
	handler := testHandler(profileGetFunc(func(context.Context, string, service.Session) (profile.Result, error) {
		t.Fatal("profile getter must not be called")
		return profile.Result{}, nil
	}), 60, 10)

	for _, body := range []string{
		`{"url":"https://linkedin.com/in/ada-example","unknown":true}`,
		`{"url":`,
		`{}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/profiles", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", body, response.Code)
		}
	}
}

func TestProfileEndpointMapsErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid URL", err: service.ErrInvalidURL, status: http.StatusBadRequest},
		{name: "invalid session", err: service.ErrInvalidSession, status: http.StatusBadRequest},
		{name: "busy", err: service.ErrBusy, status: http.StatusTooManyRequests},
		{name: "not found", err: &linkedin.Error{Kind: linkedin.ErrNotFound, Message: "missing"}, status: http.StatusNotFound},
		{name: "session", err: &linkedin.Error{Kind: linkedin.ErrAuthentication, Message: "expired"}, status: http.StatusServiceUnavailable},
		{name: "schema", err: &linkedin.Error{Kind: linkedin.ErrSchema, Message: "changed"}, status: http.StatusBadGateway},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := testHandler(profileGetFunc(func(context.Context, string, service.Session) (profile.Result, error) {
				return profile.Result{}, test.err
			}), 60, 2)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/profiles", strings.NewReader(`{"url":"https://linkedin.com/in/ada-example","linkedin_session":{"li_at":"session-value"}}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Errorf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestProfileEndpointRateLimit(t *testing.T) {
	t.Parallel()
	handler := testHandler(profileGetFunc(func(context.Context, string, service.Session) (profile.Result, error) {
		return profile.Result{Meta: profile.Meta{Warnings: []string{}}}, nil
	}), 1, 1)

	body := []byte(`{"url":"https://linkedin.com/in/ada-example","linkedin_session":{"li_at":"session-value"}}`)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestWithBody(body))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, requestWithBody(body))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}
}

func TestPublicRoutes(t *testing.T) {
	t.Parallel()
	handler := testHandler(profileGetFunc(func(context.Context, string, service.Session) (profile.Result, error) {
		return profile.Result{}, errors.New("not called")
	}), 60, 2)

	for _, path := range []string{"/", "/healthz", "/openapi.yaml"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d", path, response.Code)
		}
	}
}

func requestWithBody(body []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/profiles", bytes.NewReader(body))
	request.RemoteAddr = "192.0.2.10:1234"
	return request
}

func testHandler(getter ProfileGetter, rpm, burst int) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(getter, logger, Options{
		RateLimitRPM:   rpm,
		RateLimitBurst: burst,
		RequestTimeout: time.Second,
	})
}
