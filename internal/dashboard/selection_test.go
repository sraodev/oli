package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSelectionEndpointsRejectUnsafeRequests(t *testing.T) {
	for _, endpoint := range []string{"/api/selection", "/api/clean"} {
		for _, fields := range []string{
			`"candidate_ids":[]`, `"candidate_ids":null`, `"candidate_ids":["a","a"]`,
			`"candidate_ids":["/tmp/data"]`, `"candidate_ids":["a"],"path":"/tmp"`,
			`"candidate_ids":["a"],"rule_ids":["cache"]`,
		} {
			body := `{"scan_id":"scan-1",` + fields
			if endpoint == "/api/clean" {
				body += `,"confirmation":"DELETE"`
			}
			body += `}`
			rr := httptest.NewRecorder()
			newTestHandler(t, &fakeService{}).ServeHTTP(rr, localJSONRequest(http.MethodPost, endpoint, body))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s body=%s status=%d", endpoint, body, rr.Code)
			}
		}
	}
	for _, test := range []struct {
		token, origin string
		status        int
	}{
		{"", "", http.StatusUnauthorized}, {testToken, "https://evil.example", http.StatusForbidden},
		{testToken, "", http.StatusOK},
	} {
		req := localJSONRequest(http.MethodPost, "/api/selection", `{"scan_id":"scan-1","candidate_ids":["a"]}`)
		req.Header.Set("Authorization", "Bearer "+test.token)
		req.Header.Set("Origin", test.origin)
		rr := httptest.NewRecorder()
		newTestHandler(t, &fakeService{}).ServeHTTP(rr, req)
		if rr.Code != test.status {
			t.Fatalf("token/origin gate status=%d want=%d", rr.Code, test.status)
		}
	}
}

func TestCandidateSelectionRequestLimit(t *testing.T) {
	for _, count := range []int{128, 129} {
		ids := make([]string, count)
		for i := range ids {
			ids[i] = fmt.Sprintf("candidate-%d", i)
		}
		for _, endpoint := range []string{"/api/selection", "/api/clean"} {
			body := map[string]any{"scan_id": "scan-1", "candidate_ids": ids}
			if endpoint == "/api/clean" {
				body["confirmation"] = "DELETE"
			}
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			newTestHandler(t, &fakeService{}).ServeHTTP(rr, localJSONRequest(http.MethodPost, endpoint, string(data)))
			want := http.StatusOK
			if count > 128 {
				want = http.StatusBadRequest
			}
			if rr.Code != want {
				t.Fatalf("%s count=%d status=%d", endpoint, count, rr.Code)
			}
		}
	}
}
