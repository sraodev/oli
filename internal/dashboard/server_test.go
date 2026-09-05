package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sraodev/oli/internal/cleanup"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeService struct {
	diskFn    func(context.Context) (DiskUsage, error)
	scanFn    func(context.Context, ScanRequest, func(ScanEvent) error) error
	cleanFn   func(context.Context, CleanRequest, func(CleanEvent) error) error
	exploreFn func(context.Context, string) (*cleanup.Exploration, error)
}

func (f *fakeService) Explore(ctx context.Context, scope string) (*cleanup.Exploration, error) {
	if f.exploreFn != nil {
		return f.exploreFn(ctx, scope)
	}
	return &cleanup.Exploration{Action: cleanup.ActionScanOnly, Scope: scope}, nil
}

func TestExploreEndpointIsAuthenticatedAndPathFree(t *testing.T) {
	handler := newTestHandler(t, &fakeService{})
	for _, test := range []struct {
		body   string
		token  string
		status int
	}{
		{`{"scope":"downloads"}`, "", http.StatusUnauthorized},
		{`{"scope":"downloads"}`, testToken, http.StatusOK},
		{`{"scope":"../../"}`, testToken, http.StatusBadRequest},
		{`{"scope":"downloads","path":"/tmp"}`, testToken, http.StatusBadRequest},
		{`{"scope":"downloads","apply":true}`, testToken, http.StatusBadRequest},
		{`null`, testToken, http.StatusBadRequest},
	} {
		request := localRequest(http.MethodPost, "/api/explore", test.body)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+test.token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("body=%s status=%d want=%d", test.body, response.Code, test.status)
		}
	}
}

func TestExploreEndpointPreservesPartialReportAndSetsDeadline(t *testing.T) {
	service := &fakeService{exploreFn: func(ctx context.Context, scope string) (*cleanup.Exploration, error) {
		deadline, ok := ctx.Deadline()
		if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 2*time.Minute {
			t.Fatalf("deadline=%v present=%v", deadline, ok)
		}
		return &cleanup.Exploration{Action: cleanup.ActionScanOnly, Scope: scope, Partial: true,
			Warnings: []cleanup.Warning{{Code: "entry_limit", Message: "Entry limit reached."}}}, nil
	}}
	response := httptest.NewRecorder()
	newTestHandler(t, service).ServeHTTP(response, localJSONRequest(http.MethodPost, "/api/explore", `{"scope":"downloads"}`))
	var report cleanup.Exploration
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || report.Action != cleanup.ActionScanOnly || !report.Partial || len(report.Warnings) != 1 {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}

func TestExploreEndpointCancelsWithRequestOrServer(t *testing.T) {
	for _, source := range []string{"request", "server"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			service := &fakeService{exploreFn: func(ctx context.Context, _ string) (*cleanup.Exploration, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			config := Config{Token: testToken, Service: service}
			request := localJSONRequest(http.MethodPost, "/api/explore", `{"scope":"downloads"}`)
			if source == "server" {
				config.Context = ctx
			} else {
				request = request.WithContext(ctx)
			}
			handler, err := Handler(config)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); handler.ServeHTTP(response, request) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("exploration did not start")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("exploration did not cancel")
			}
			if response.Code == http.StatusOK || strings.Contains(response.Body.String(), `"action"`) {
				t.Fatalf("cancellation returned a report: %s", response.Body.String())
			}
		})
	}
}

func (f *fakeService) Disk(ctx context.Context) (DiskUsage, error) {
	if f.diskFn != nil {
		return f.diskFn(ctx)
	}
	return DiskUsage{Volume: "Macintosh HD", TotalBytes: 1000, UsedBytes: 600, FreeBytes: 400}, nil
}

func (f *fakeService) Scan(ctx context.Context, request ScanRequest, emit func(ScanEvent) error) error {
	if f.scanFn != nil {
		return f.scanFn(ctx, request, emit)
	}
	return nil
}

func (f *fakeService) Clean(ctx context.Context, request CleanRequest, emit func(CleanEvent) error) error {
	if f.cleanFn != nil {
		return f.cleanFn(ctx, request, emit)
	}
	return nil
}

func TestHandlerRejectsMissingOrInvalidToken(t *testing.T) {
	handler := newTestHandler(t, &fakeService{})

	for name, authorization := range map[string]string{
		"missing": "",
		"wrong":   "Bearer definitely-not-the-token",
		"basic":   "Basic " + testToken,
	} {
		t.Run(name, func(t *testing.T) {
			request := localRequest(http.MethodGet, "/api/disk", "")
			request.Header.Set("Authorization", authorization)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="oli"` {
				t.Fatalf("unexpected authentication realm: %q", got)
			}
		})
	}
}

func TestHandlerRejectsNonLoopbackRequests(t *testing.T) {
	handler := newTestHandler(t, &fakeService{})
	request := httptest.NewRequest(http.MethodGet, "http://example.test/api/disk", nil)
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestSecurityHeadersAndEmbeddedAssets(t *testing.T) {
	handler := newTestHandler(t, &fakeService{})
	request := localRequest(http.MethodGet, "/", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, want := range []string{"<title>Oli — Open Lifecycle Intelligence</title>", "<strong>Oli</strong>", "LOCAL HOUSEKEEPER"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("dashboard missing brand text %q", want)
		}
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") || !strings.Contains(got, "connect-src 'self'") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("unexpected Content-Security-Policy: %q", got)
	} else if strings.Contains(got, "unsafe-inline") || strings.Contains(got, "unsafe-eval") || strings.Contains(got, "http:") || strings.Contains(got, "https:") {
		t.Fatalf("Content-Security-Policy allows an unsafe source: %q", got)
	}
	for header, want := range map[string]string{
		"Cache-Control":                "no-store",
		"Cross-Origin-Embedder-Policy": "require-corp",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if body := response.Body.String(); !strings.Contains(body, "/assets/app.js") || strings.Contains(body, "https://") || strings.Contains(body, "http://") {
		t.Fatalf("index did not use only embedded local assets")
	} else if strings.Contains(body, "<style") || strings.Contains(body, " style=") || strings.Contains(body, "<script>") {
		t.Fatalf("index contains inline executable or style content")
	}
}

func TestAPIRejectsCrossOriginRequest(t *testing.T) {
	handler := newTestHandler(t, &fakeService{})
	request := localRequest(http.MethodGet, "/api/disk", "")
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Origin", "https://malicious.example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestScanStreamsNDJSON(t *testing.T) {
	service := &fakeService{
		scanFn: func(_ context.Context, request ScanRequest, emit func(ScanEvent) error) error {
			if request.Profile != ProfileSafe {
				t.Fatalf("profile = %q, want %q", request.Profile, ProfileSafe)
			}
			for _, event := range []ScanEvent{
				{Type: "scan.started", ScanID: "scan-123"},
				{Type: "scan.category", Category: &ScanCategory{
					RuleID: "archives", Name: "Large archives", Risk: RiskHigh, Action: ActionScanOnly,
					DetectedBytes: 5 << 30, ReclaimableBytes: 0, ItemCount: 3,
				}},
				{Type: "scan.complete", Summary: &ScanSummary{ScanID: "scan-123", ReclaimableBytes: 0}},
			} {
				if err := emit(event); err != nil {
					return err
				}
			}
			return nil
		},
	}
	handler := newTestHandler(t, service)
	request := localJSONRequest(http.MethodPost, "/api/scan", `{"profile":"safe"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/x-ndjson") {
		t.Fatalf("Content-Type = %q", got)
	}
	lines := nonEmptyLines(response.Body.String())
	if len(lines) != 3 {
		t.Fatalf("got %d stream events, want 3: %s", len(lines), response.Body.String())
	}
	var categoryEvent ScanEvent
	if err := json.Unmarshal([]byte(lines[1]), &categoryEvent); err != nil {
		t.Fatalf("decode category event: %v", err)
	}
	if categoryEvent.Category == nil || categoryEvent.Category.DetectedBytes != 5<<30 || categoryEvent.Category.ReclaimableBytes != 0 || categoryEvent.Category.Action != ActionScanOnly {
		t.Fatalf("unexpected category event: %#v", categoryEvent.Category)
	}
}

func TestRequestValidationRejectsPathsAndInvalidIDs(t *testing.T) {
	scanCalls := 0
	cleanCalls := 0
	service := &fakeService{
		scanFn: func(context.Context, ScanRequest, func(ScanEvent) error) error {
			scanCalls++
			return nil
		},
		cleanFn: func(context.Context, CleanRequest, func(CleanEvent) error) error {
			cleanCalls++
			return nil
		},
	}
	handler := newTestHandler(t, service)

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "scan path field", path: "/api/scan", body: `{"profile":"safe","path":"/Users/example"}`},
		{name: "scan null", path: "/api/scan", body: `null`},
		{name: "unknown profile", path: "/api/scan", body: `{"profile":"everything"}`},
		{name: "clean path field", path: "/api/clean", body: `{"scan_id":"scan-1","rule_ids":["cache"],"confirmation":"DELETE","path":"/tmp"}`},
		{name: "clean null", path: "/api/clean", body: `null`},
		{name: "path-shaped rule ID", path: "/api/clean", body: `{"scan_id":"scan-1","rule_ids":["../../private"],"confirmation":"DELETE"}`},
		{name: "duplicate rule ID", path: "/api/clean", body: `{"scan_id":"scan-1","rule_ids":["cache","cache"],"confirmation":"DELETE"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := localJSONRequest(http.MethodPost, test.path, test.body)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
	if scanCalls != 0 || cleanCalls != 0 {
		t.Fatalf("service called for rejected request: scan=%d clean=%d", scanCalls, cleanCalls)
	}
}

func TestCleanRequiresTypedConfirmationAndStreamsOutcome(t *testing.T) {
	cleanCalls := 0
	var received CleanRequest
	service := &fakeService{
		cleanFn: func(_ context.Context, request CleanRequest, emit func(CleanEvent) error) error {
			cleanCalls++
			received = request
			return emit(CleanEvent{Type: "clean.complete", Summary: &CleanSummary{
				SelectedBytes: 2048, RemovedBytes: 1900, MeasuredReclaimedBytes: 1800,
			}})
		},
	}
	handler := newTestHandler(t, service)

	bad := localJSONRequest(http.MethodPost, "/api/clean", `{"scan_id":"scan-1","rule_ids":["user-cache"],"confirmation":"delete"}`)
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusBadRequest || cleanCalls != 0 {
		t.Fatalf("invalid confirmation status=%d cleanCalls=%d", badResponse.Code, cleanCalls)
	}

	good := localJSONRequest(http.MethodPost, "/api/clean", `{"scan_id":"scan-1","rule_ids":["user-cache"],"confirmation":"DELETE"}`)
	goodResponse := httptest.NewRecorder()
	handler.ServeHTTP(goodResponse, good)

	if goodResponse.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", goodResponse.Code, http.StatusOK, goodResponse.Body.String())
	}
	if cleanCalls != 1 {
		t.Fatalf("clean calls = %d, want 1", cleanCalls)
	}
	if received.ScanID != "scan-1" || len(received.RuleIDs) != 1 || received.RuleIDs[0] != "user-cache" || received.Confirmation != confirmationWord {
		t.Fatalf("unexpected clean request: %#v", received)
	}
	if !strings.Contains(goodResponse.Body.String(), `"measured_reclaimed_bytes":1800`) {
		t.Fatalf("clean stream missing measured outcome: %s", goodResponse.Body.String())
	}
}

func TestServiceErrorsAreNotExposed(t *testing.T) {
	service := &fakeService{
		diskFn: func(context.Context) (DiskUsage, error) {
			return DiskUsage{}, errors.New("/Users/private/secret: permission denied")
		},
	}
	handler := newTestHandler(t, service)
	request := localRequest(http.MethodGet, "/api/disk", "")
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "/Users/") || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("internal service error leaked: %s", response.Body.String())
	}
}

func TestNewServerRequiresExplicitLoopbackAddress(t *testing.T) {
	config := Config{Token: testToken, Service: &fakeService{}}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.0.2.1:8080", "bad-address"} {
		if _, err := NewServer(addr, config); err == nil {
			t.Errorf("NewServer(%q) succeeded, want error", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:8080", "[::1]:0"} {
		server, err := NewServer(addr, config)
		if err != nil {
			t.Errorf("NewServer(%q): %v", addr, err)
			continue
		}
		if server.Addr != addr || server.Handler == nil {
			t.Errorf("unexpected server for %q: %#v", addr, server)
		}
	}
}

func TestHandlerCancelsOperationWithConfiguredContext(t *testing.T) {
	baseContext, cancelBase := context.WithCancel(context.Background())
	started := make(chan struct{})
	service := &fakeService{
		scanFn: func(ctx context.Context, _ ScanRequest, _ func(ScanEvent) error) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	handler, err := Handler(Config{Context: baseContext, Token: testToken, Service: service})
	if err != nil {
		t.Fatal(err)
	}
	request := localJSONRequest(http.MethodPost, "/api/scan", `{"profile":"safe"}`)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan service did not start")
	}
	cancelBase()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("configured context did not cancel scan service")
	}
	if strings.Contains(response.Body.String(), "scan.issue") {
		t.Fatalf("context cancellation was misreported as a scan issue: %s", response.Body.String())
	}
}

func TestNewServerShutdownCancelsBaseContext(t *testing.T) {
	server, err := NewServer("127.0.0.1:0", Config{Token: testToken, Service: &fakeService{}})
	if err != nil {
		t.Fatal(err)
	}
	baseContext := server.BaseContext(nil)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-baseContext.Done():
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not cancel its base context")
	}
}

func newTestHandler(t *testing.T, service Service) http.Handler {
	t.Helper()
	handler, err := Handler(Config{Token: testToken, Service: service})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return handler
}

func localRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, "http://127.0.0.1:4173"+target, strings.NewReader(body))
	request.Host = "127.0.0.1:4173"
	request.RemoteAddr = "127.0.0.1:54321"
	return request
}

func localJSONRequest(method, target, body string) *http.Request {
	request := localRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func nonEmptyLines(value string) []string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}
