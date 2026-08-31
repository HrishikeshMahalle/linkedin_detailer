package linkedin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientFetchProfile(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile("testdata/dash_profile.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/voyager/api/identity/dash/profiles" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "memberIdentity" || r.URL.Query().Get("memberIdentity") != "ada-example" {
			t.Errorf("profile query = %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("decorationId") != defaultDecoration {
			t.Errorf("decorationId = %q", r.URL.Query().Get("decorationId"))
		}
		if r.Header.Get("csrf-token") != "ajax:123" {
			t.Errorf("csrf-token = %q", r.Header.Get("csrf-token"))
		}
		cookie, cookieErr := r.Cookie("li_at")
		if cookieErr != nil || cookie.Value != "session-value" {
			t.Errorf("li_at cookie = %#v, error = %v", cookie, cookieErr)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client, err := NewClient(Options{
		LIAT:       "session-value",
		JSESSIONID: `"ajax:123"`,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Interval:   time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, warnings, err := client.FetchProfile(context.Background(), "ada-example")
	if err != nil {
		t.Fatalf("FetchProfile() error = %v", err)
	}
	if result.FullName != "Ada Example" || len(warnings) != 0 {
		t.Errorf("FetchProfile() result = %#v, warnings = %v", result, warnings)
	}
}

func TestClientClassifiesBlockingResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantKind    ErrorKind
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, contentType: "application/json", body: `{}`, wantKind: ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, contentType: "application/json", body: `{}`, wantKind: ErrForbidden},
		{name: "not found", status: http.StatusNotFound, contentType: "application/json", body: `{}`, wantKind: ErrNotFound},
		{name: "rate limited", status: http.StatusTooManyRequests, contentType: "application/json", body: `{}`, wantKind: ErrRateLimited},
		{name: "retired query", status: http.StatusGone, contentType: "application/json", body: `{}`, wantKind: ErrSchema},
		{name: "challenge HTML", status: http.StatusOK, contentType: "text/html", body: `<title>Security challenge</title>`, wantKind: ErrChallenge},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewClient(Options{
				LIAT:       "session-value",
				JSESSIONID: "ajax:123",
				BaseURL:    server.URL,
				HTTPClient: server.Client(),
				Interval:   time.Nanosecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.FetchProfile(context.Background(), "ada-example")
			if err == nil {
				t.Fatal("FetchProfile() error = nil")
			}
			if got := KindOf(err); got != test.wantKind {
				t.Errorf("KindOf(error) = %q, want %q; error = %v", got, test.wantKind, err)
			}
		})
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"profile":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseSize)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{
		LIAT:       "session-value",
		JSESSIONID: "ajax:123",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Interval:   time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.FetchProfile(context.Background(), "ada-example")
	if err == nil {
		t.Fatal("FetchProfile() error = nil, want response-size error")
	}
	var linkedInErr *Error
	if !errors.As(err, &linkedInErr) || linkedInErr.Kind != ErrUpstream {
		t.Fatalf("FetchProfile() error = %v, want upstream Error", err)
	}
}
