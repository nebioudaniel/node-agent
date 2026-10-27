package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"node-agent/internal/specs"
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
