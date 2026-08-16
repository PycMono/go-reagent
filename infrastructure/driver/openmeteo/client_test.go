package openmeteo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/domain/service"
)

const geocodingFixture = `{
  "results": [{
    "name": "Beijing", "country": "China", "country_code": "CN",
    "admin1": "Beijing", "latitude": 39.9042, "longitude": 116.4074,
    "timezone": "Asia/Shanghai"
  }]
}`

func TestResolveLocationsMapsCandidatesAndEncodesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" || r.URL.Query().Get("name") != "北京" ||
			r.URL.Query().Get("count") != "5" || r.Header.Get("User-Agent") == "" {
			t.Fatalf("request = %s %#v", r.URL.String(), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, geocodingFixture)
	}))
	defer server.Close()

	client := newClient(server.URL+"/v1/search", server.URL+"/v1/forecast", server.Client())
	got, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "北京", Limit: 9})
	if err != nil {
		t.Fatal(err)
	}
	want := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Admin1: "Beijing", Latitude: 39.9042, Longitude: 116.4074, Timezone: "Asia/Shanghai"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("locations = %#v", got)
	}
}

func TestResolveLocationsMapsEmptyResult(t *testing.T) {
	client := clientForGeocodingFixture(t, http.StatusOK, `{"generationtime_ms":0.1}`)
	got, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "missing", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("locations = %#v, want non-nil empty slice", got)
	}
}

func TestResolveLocationsRejectsInvalidUpstreamResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		fixture string
		want    string
	}{
		{name: "non success", status: http.StatusBadGateway, fixture: `secret-response-body`, want: "HTTP 502"},
		{name: "malformed JSON", status: http.StatusOK, fixture: `{"results":`, want: "invalid JSON"},
		{name: "trailing JSON", status: http.StatusOK, fixture: `{"results":[]} {}`, want: "trailing JSON"},
		{name: "missing name", status: http.StatusOK, fixture: `{"results":[{"country":"China","country_code":"CN","admin1":"Beijing","latitude":39.9,"longitude":116.4,"timezone":"Asia/Shanghai"}]}`, want: "missing required fields"},
		{name: "missing country", status: http.StatusOK, fixture: `{"results":[{"name":"Beijing","country_code":"CN","admin1":"Beijing","latitude":39.9,"longitude":116.4,"timezone":"Asia/Shanghai"}]}`, want: "missing required fields"},
		{name: "missing country code", status: http.StatusOK, fixture: `{"results":[{"name":"Beijing","country":"China","admin1":"Beijing","latitude":39.9,"longitude":116.4,"timezone":"Asia/Shanghai"}]}`, want: "missing required fields"},
		{name: "missing timezone", status: http.StatusOK, fixture: `{"results":[{"name":"Beijing","country":"China","country_code":"CN","admin1":"Beijing","latitude":39.9,"longitude":116.4}]}`, want: "missing required fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientForGeocodingFixture(t, test.status, test.fixture)
			_, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "query-secret", Limit: 1})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want category %q", err, test.want)
			}
			for _, secret := range []string{"secret-response-body", "query-secret", "/v1/search?"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestResolveLocationsRejectsOversizedResponse(t *testing.T) {
	client := clientForGeocodingFixture(t, http.StatusOK, strings.Repeat("x", (1<<20)+1))
	_, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "Beijing", Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveLocationsRejectsRedirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		_, _ = io.WriteString(w, geocodingFixture)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	client := newClient(redirect.URL, redirect.URL, &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	_, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "Beijing", Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("error = %v", err)
	}
	if targetCalls != 0 {
		t.Fatalf("redirect target calls = %d", targetCalls)
	}
}

func TestResolveLocationsHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()

	client := newClient(server.URL, server.URL, server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ResolveLocations(ctx, service.LocationQuery{Name: "Beijing", Limit: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestResolveLocationsHonorsHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Timeout = 10 * time.Millisecond
	client := newClient(server.URL, server.URL, httpClient)
	_, err := client.ResolveLocations(context.Background(), service.LocationQuery{Name: "Beijing", Limit: 1})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func clientForGeocodingFixture(t *testing.T, status int, fixture string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, fixture)
	}))
	t.Cleanup(server.Close)
	return newClient(server.URL+"/v1/search", server.URL+"/v1/forecast", server.Client())
}
