package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tradelab/linkedin-profile-api/internal/profile"
)

type fetchFunc func(context.Context, string) (profile.Profile, []string, error)

func (f fetchFunc) FetchProfile(ctx context.Context, identifier string) (profile.Profile, []string, error) {
	return f(ctx, identifier)
}

var testSession = Session{LIAT: "session-one", JSESSIONID: "ajax:one"}

func testFactory(fetcher Fetcher) FetcherFactory {
	return FetcherFactoryFunc(func(Session) (Fetcher, error) {
		return fetcher, nil
	})
}

func TestParseProfileURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "www", raw: "https://www.linkedin.com/in/Ada-Example", want: "ada-example"},
		{name: "tracking and slash", raw: "https://linkedin.com/in/ada-example/?trk=public_profile", want: "ada-example"},
		{name: "country subdomain", raw: "https://uk.linkedin.com/in/ada_example", want: "ada_example"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseProfileURL(test.raw)
			if err != nil {
				t.Fatalf("ParseProfileURL() error = %v", err)
			}
			if got != test.want {
				t.Errorf("ParseProfileURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseProfileURLRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	values := []string{
		"",
		"http://www.linkedin.com/in/ada-example",
		"https://linkedin.com.evil.test/in/ada-example",
		"https://user@linkedin.com/in/ada-example",
		"https://linkedin.com:8443/in/ada-example",
		"https://linkedin.com/company/example",
		"https://linkedin.com/in/ada-example/extra",
		"https://127.0.0.1/in/ada-example",
		"https://linkedin.com/in/a%2Fb",
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseProfileURL(value); !errors.Is(err, ErrInvalidURL) {
				t.Errorf("ParseProfileURL(%q) error = %v, want ErrInvalidURL", value, err)
			}
		})
	}
}

func TestProfileServiceCachesResults(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fetcher := fetchFunc(func(_ context.Context, identifier string) (profile.Profile, []string, error) {
		calls.Add(1)
		return profile.Profile{PublicIdentifier: identifier}, nil, nil
	})
	service := NewProfileService(testFactory(fetcher), time.Minute, 10, 1)

	first, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", testSession)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Get(context.Background(), "https://www.linkedin.com/in/ada-example?trk=test", testSession)
	if err != nil {
		t.Fatal(err)
	}
	if first.Meta.CacheHit {
		t.Error("first response should not be a cache hit")
	}
	if !second.Meta.CacheHit {
		t.Error("second response should be a cache hit")
	}
	if calls.Load() != 1 {
		t.Errorf("fetch calls = %d, want 1", calls.Load())
	}
}

func TestProfileServiceCoalescesDuplicateRequests(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fetcher := fetchFunc(func(_ context.Context, identifier string) (profile.Profile, []string, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond)
		return profile.Profile{PublicIdentifier: identifier}, nil, nil
	})
	service := NewProfileService(testFactory(fetcher), time.Minute, 10, 1)

	const count = 8
	start := make(chan struct{})
	errs := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", testSession)
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("fetch calls = %d, want 1", calls.Load())
	}
}

func TestProfileServiceRejectsExcessConcurrency(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	fetcher := fetchFunc(func(_ context.Context, identifier string) (profile.Profile, []string, error) {
		if identifier == "first-profile" {
			close(started)
			<-release
		}
		return profile.Profile{PublicIdentifier: identifier}, nil, nil
	})
	service := NewProfileService(testFactory(fetcher), time.Minute, 10, 1)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Get(context.Background(), "https://linkedin.com/in/first-profile", testSession)
		firstDone <- err
	}()
	<-started

	_, err := service.Get(context.Background(), "https://linkedin.com/in/second-profile", testSession)
	if !errors.Is(err, ErrBusy) {
		t.Errorf("Get() error = %v, want ErrBusy", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Errorf("first Get() error = %v", err)
	}
}

func TestProfileServiceMarksWarningsPartial(t *testing.T) {
	t.Parallel()
	fetcher := fetchFunc(func(_ context.Context, identifier string) (profile.Profile, []string, error) {
		return profile.Profile{PublicIdentifier: identifier}, []string{"skills unavailable"}, nil
	})
	service := NewProfileService(testFactory(fetcher), time.Minute, 10, 1)

	result, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", testSession)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Meta.Partial || len(result.Meta.Warnings) != 1 {
		t.Errorf("Meta = %#v, want partial result", result.Meta)
	}
}

func TestProfileServiceRequiresSession(t *testing.T) {
	t.Parallel()
	service := NewProfileService(testFactory(fetchFunc(func(context.Context, string) (profile.Profile, []string, error) {
		t.Fatal("fetcher must not be called")
		return profile.Profile{}, nil, nil
	})), time.Minute, 10, 1)

	_, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", Session{})
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("Get() error = %v, want ErrInvalidSession", err)
	}
}

func TestProfileServiceIsolatesSessions(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	factory := FetcherFactoryFunc(func(session Session) (Fetcher, error) {
		return fetchFunc(func(_ context.Context, identifier string) (profile.Profile, []string, error) {
			calls.Add(1)
			return profile.Profile{PublicIdentifier: identifier, Headline: session.LIAT}, nil, nil
		}), nil
	})
	service := NewProfileService(factory, time.Minute, 10, 1)

	first, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", Session{LIAT: "session-one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", Session{LIAT: "session-two"})
	if err != nil {
		t.Fatal(err)
	}
	firstCached, err := service.Get(context.Background(), "https://linkedin.com/in/ada-example", Session{LIAT: "session-one"})
	if err != nil {
		t.Fatal(err)
	}

	if first.Profile.Headline != "session-one" || second.Profile.Headline != "session-two" {
		t.Fatal("profile data crossed session boundaries")
	}
	if !firstCached.Meta.CacheHit || calls.Load() != 2 {
		t.Errorf("cache isolation failed: cache_hit=%v calls=%d", firstCached.Meta.CacheHit, calls.Load())
	}
}
