package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"node-agent/internal/specs"
	"testing"
)

func TestSpecsHandlerReturns500OnCollectionError(t *testing.T) {
	// 1. Create a collector that deliberately fails.
	collect := func() (specs.Specs, error) {
		return specs.Specs{}, errors.New("collection failed")
	}

	// 2. Give the failing collector to our HTTP server.
	handler := NewHandler(collect)

	// 3. Pretend a client requests /specs.
	req := httptest.NewRequest(http.MethodGet, "/specs", nil)
	rec := httptest.NewRecorder()

	// 4. Run the handler and capture its response.
	handler.ServeHTTP(rec, req)

	// 5. Check that the response status is HTTP 500.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf(
			"got status %d, want %d",
			rec.Code,
			http.StatusInternalServerError,
		)
	}
}
func TestHealthHandler(t *testing.T) {
	// 1. Create a handler with a collector.
	collect := func() (specs.Specs, error) {
		return specs.Specs{}, nil
	}

	handler := NewHandler(collect)

	// 2. Simulate a request to /health.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// 3. Run the handler and capture its response.
	handler.ServeHTTP(rec, req)

	// 4. Check that the status is 200 OK.
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// 5. Check the response body.
	want := `{"status":"ok"}`
	if rec.Body.String() != want {
		t.Fatalf("got body %q, want %q", rec.Body.String(), want)
	}
}

// 1. Define the specs we expect.
func TestSpecsHandlerReturnsSpecs(t *testing.T) {
	want := specs.Specs{
		Hostname: "test-computer",
		OS:       "darwin",
		Arch:     "amd64",
		CPU:      "Test CPU",
		Cores:    4,
		TotalRAM: 8_000_000_000,
	}

	// 2. Create a fake collector that returns our sample specs.
	collect := func() (specs.Specs, error) {
		return want, nil
	}

	// 3. Create the HTTP handler.
	handler := NewHandler(collect)

	// 4. Simulate a GET request to /specs.
	req := httptest.NewRequest(http.MethodGet, "/specs", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// 5. Check the HTTP status.
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// 6. Decode the JSON response into a Specs struct.
	var got specs.Specs
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	// 7. Compare the actual specs with the expected specs.
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
