package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRouter(t *testing.T) {
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>hello</h1>")}}
	h := NewRouter(static)

	tests := []struct {
		path, want string
	}{
		{"/", "hello"},
		{"/healthz", `"ok"`},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: code=%d body=%q", tc.path, rec.Code, rec.Body.String())
		}
	}
}
