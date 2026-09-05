// Package dashboard serves the local Oli user interface.
//
// The package deliberately knows nothing about the filesystem. Callers provide
// a Service implementation, and mutation requests identify previously scanned
// rules instead of accepting paths.
package dashboard

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sraodev/oli/internal/cleanup"
)

const (
	ProfileSafe     = "safe"
	ProfileBalanced = "balanced"
	ProfileReview   = "review"

	confirmationWord = "DELETE"
	maxRequestBytes  = 16 << 10
)

//go:embed static/*
var staticFiles embed.FS

// Config contains the dependencies required by Handler and NewServer.
type Config struct {
	// Context bounds all service operations. NewServer also uses it as the HTTP
	// base context. A nil context defaults to context.Background().
	Context context.Context
	// Token authenticates every /api request. Generate a cryptographically
	// random value for each server run and pass it to the page in the URL
	// fragment: /#token=<token>.
	Token string
	// Service performs scans and cleanups. The dashboard never accesses the
	// filesystem directly.
	Service Service
}

// Service is the backend contract consumed by the dashboard. Implementations
// should stop promptly when ctx is cancelled and must serialize calls to emit.
type Service interface {
	Disk(context.Context) (DiskUsage, error)
	Scan(context.Context, ScanRequest, func(ScanEvent) error) error
	Clean(context.Context, CleanRequest, func(CleanEvent) error) error
	Explore(context.Context, string) (*cleanup.Exploration, error)
}

// DiskUsage is the measured capacity of the volume being cleaned.
type DiskUsage struct {
	Volume     string `json:"volume"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// ScanRequest selects a bounded, server-defined scan profile.
type ScanRequest struct {
	Profile string `json:"profile"`
}

// Risk communicates the review level of a cleanup rule.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// RuleAction determines whether a scanned rule may be sent to Clean. It is an
// alias so adapters can pass the cleanup core's serialized action directly.
type RuleAction = string

const (
	ActionClean    RuleAction = "clean"
	ActionScanOnly RuleAction = "scan_only"
)

// ScanEvent is one line in the scan NDJSON stream. Supported event types are
// scan.started, scan.progress, scan.category, scan.issue, and scan.complete.
type ScanEvent struct {
	Type             string        `json:"type"`
	ScanID           string        `json:"scan_id,omitempty"`
	Message          string        `json:"message,omitempty"`
	FilesScanned     uint64        `json:"files_scanned,omitempty"`
	BytesScanned     uint64        `json:"bytes_scanned,omitempty"`
	ReclaimableBytes uint64        `json:"reclaimable_bytes,omitempty"`
	Progress         float64       `json:"progress,omitempty"`
	Category         *ScanCategory `json:"category,omitempty"`
	Issue            *ScanIssue    `json:"issue,omitempty"`
	Summary          *ScanSummary  `json:"summary,omitempty"`
}

// ScanCategory describes one server-owned cleanup rule.
type ScanCategory struct {
	RuleID            string     `json:"rule_id"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Risk              Risk       `json:"risk"`
	Age               string     `json:"age"`
	Action            RuleAction `json:"action"`
	ItemCount         uint64     `json:"item_count"`
	DetectedBytes     uint64     `json:"detected_bytes"`
	ReclaimableBytes  uint64     `json:"reclaimable_bytes"`
	SelectedByDefault bool       `json:"selected_by_default"`
	LargestItems      []ScanItem `json:"largest_items,omitempty"`
}

// ScanItem is display-only metadata for an item found by a rule. It is never
// accepted by a mutation endpoint.
type ScanItem struct {
	Name        string `json:"name"`
	Location    string `json:"location,omitempty"`
	SizeBytes   uint64 `json:"size_bytes"`
	ModifiedAge string `json:"modified_age,omitempty"`
}

// ScanIssue reports a rule that could not be fully inspected.
type ScanIssue struct {
	RuleID      string `json:"rule_id,omitempty"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// ScanSummary closes a successful scan.
type ScanSummary struct {
	ScanID           string `json:"scan_id"`
	FilesScanned     uint64 `json:"files_scanned"`
	BytesScanned     uint64 `json:"bytes_scanned"`
	ReclaimableBytes uint64 `json:"reclaimable_bytes"`
	DurationMillis   int64  `json:"duration_millis"`
}

// CleanRequest can reference only a completed scan and rules returned by that
// scan. Confirmation must be exactly "DELETE".
type CleanRequest struct {
	ScanID       string   `json:"scan_id"`
	RuleIDs      []string `json:"rule_ids"`
	Confirmation string   `json:"confirmation"`
}

// CleanEvent is one line in the clean NDJSON stream. Supported event types are
// clean.started, clean.progress, clean.issue, clean.partial, and clean.complete.
type CleanEvent struct {
	Type         string        `json:"type"`
	RuleID       string        `json:"rule_id,omitempty"`
	Message      string        `json:"message,omitempty"`
	Progress     float64       `json:"progress,omitempty"`
	RemovedItems uint64        `json:"removed_items,omitempty"`
	RemovedBytes uint64        `json:"removed_bytes,omitempty"`
	Issue        *CleanIssue   `json:"issue,omitempty"`
	Summary      *CleanSummary `json:"summary,omitempty"`
}

// CleanIssue reports an item or rule that could not be cleaned.
type CleanIssue struct {
	RuleID  string `json:"rule_id,omitempty"`
	Message string `json:"message"`
}

// CleanSummary reports both the selected estimate and the measured disk-space
// outcome. MeasuredReclaimedBytes should be based on volume readings before and
// after cleanup rather than on summed file sizes.
type CleanSummary struct {
	SelectedBytes          uint64 `json:"selected_bytes"`
	RemovedBytes           uint64 `json:"removed_bytes"`
	MeasuredReclaimedBytes uint64 `json:"measured_reclaimed_bytes"`
	RemovedItems           uint64 `json:"removed_items"`
	FailedItems            uint64 `json:"failed_items"`
	BeforeFreeBytes        uint64 `json:"before_free_bytes"`
	AfterFreeBytes         uint64 `json:"after_free_bytes"`
	DurationMillis         int64  `json:"duration_millis"`
	Partial                bool   `json:"partial,omitempty"`
}

// Handler constructs the local dashboard HTTP handler.
func Handler(config Config) (http.Handler, error) {
	if config.Service == nil {
		return nil, errors.New("dashboard: service is required")
	}
	if len(config.Token) < 16 {
		return nil, errors.New("dashboard: token must contain at least 16 characters")
	}
	baseContext := config.Context
	if baseContext == nil {
		baseContext = context.Background()
	}

	app := &appHandler{
		service:     config.Service,
		baseContext: baseContext,
		tokenHash:   sha256.Sum256([]byte(config.Token)),
	}
	return securityHeaders(localOnly(app)), nil
}

// NewServer constructs an HTTP server restricted to an explicit loopback
// address. It does not start the server.
func NewServer(addr string, config Config) (*http.Server, error) {
	if !isLoopbackAddress(addr) {
		return nil, fmt.Errorf("dashboard: address %q is not an explicit loopback address", addr)
	}
	baseContext := config.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	serverContext, cancelServer := context.WithCancel(baseContext)
	config.Context = serverContext
	handler, err := Handler(config)
	if err != nil {
		cancelServer()
		return nil, err
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return serverContext },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	server.RegisterOnShutdown(cancelServer)
	return server, nil
}

type appHandler struct {
	service     Service
	baseContext context.Context
	tokenHash   [sha256.Size]byte
}

func (a *appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if !a.authenticated(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="oli"`)
			writeError(w, http.StatusUnauthorized, "A valid local session token is required.")
			return
		}
		if !sameOriginRequest(r) {
			writeError(w, http.StatusForbidden, "Cross-origin requests are not allowed.")
			return
		}
	}

	switch r.URL.Path {
	case "/api/disk":
		a.handleDisk(w, r)
	case "/api/scan":
		a.handleScan(w, r)
	case "/api/explore":
		a.handleExplore(w, r)
	case "/api/clean":
		a.handleClean(w, r)
	case "/", "/index.html":
		serveEmbedded(w, r, "static/index.html", "text/html; charset=utf-8")
	case "/assets/app.css":
		serveEmbedded(w, r, "static/app.css", "text/css; charset=utf-8")
	case "/assets/app.js":
		serveEmbedded(w, r, "static/app.js", "text/javascript; charset=utf-8")
	case "/favicon.ico":
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusNotFound, "Not found.")
	}
}

func (a *appHandler) authenticated(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	provided := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(provided[:], a.tokenHash[:]) == 1
}

func (a *appHandler) handleDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	ctx, cancel := a.operationContext(r.Context())
	defer cancel()
	disk, err := a.service.Disk(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Disk usage is temporarily unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, disk)
}

func (a *appHandler) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}
	var decoded *ScanRequest
	if err := decodeRequest(w, r, &decoded); err != nil || decoded == nil {
		if err == nil {
			err = errors.New("Request body must be one valid JSON object with only supported fields.")
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request := *decoded
	if request.Profile == "" {
		request.Profile = ProfileSafe
	}
	if !validProfile(request.Profile) {
		writeError(w, http.StatusBadRequest, "profile must be safe, balanced, or review.")
		return
	}

	startNDJSON(w)
	ctx, cancel := a.operationContext(r.Context())
	defer cancel()
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	emit := func(event ScanEvent) error {
		if event.Type == "" {
			return errors.New("dashboard: scan event type is required")
		}
		if err := encoder.Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	if err := a.service.Scan(ctx, request, emit); err != nil && ctx.Err() == nil {
		_ = emit(ScanEvent{Type: "scan.issue", Issue: &ScanIssue{
			Message: "The scan stopped before it could finish.",
		}})
	}
}

func (a *appHandler) handleExplore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}
	var request *struct {
		Scope string `json:"scope"`
	}
	if err := decodeRequest(w, r, &request); err != nil || request == nil || !cleanup.ValidExploreScope(request.Scope) {
		writeError(w, http.StatusBadRequest, "Choose one supported scope; paths and cleanup flags are not accepted.")
		return
	}
	ctx, cancel := a.operationContext(r.Context())
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 2*time.Minute)
	defer timeout()
	report, err := a.service.Explore(ctx, request.Scope)
	if err != nil {
		writeError(w, http.StatusConflict, "Exploration could not finish. Wait for other operations, then try a narrower scope.")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *appHandler) handleClean(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}
	var decoded *CleanRequest
	if err := decodeRequest(w, r, &decoded); err != nil || decoded == nil {
		if err == nil {
			err = errors.New("Request body must be one valid JSON object with only supported fields.")
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request := *decoded
	if request.Confirmation != confirmationWord {
		writeError(w, http.StatusBadRequest, `confirmation must be exactly "DELETE".`)
		return
	}
	if !validIdentifier(request.ScanID) {
		writeError(w, http.StatusBadRequest, "scan_id is invalid.")
		return
	}
	if len(request.RuleIDs) == 0 || len(request.RuleIDs) > 128 {
		writeError(w, http.StatusBadRequest, "rule_ids must contain between 1 and 128 rules.")
		return
	}
	seen := make(map[string]struct{}, len(request.RuleIDs))
	for _, ruleID := range request.RuleIDs {
		if !validIdentifier(ruleID) {
			writeError(w, http.StatusBadRequest, "rule_ids contains an invalid rule ID.")
			return
		}
		if _, exists := seen[ruleID]; exists {
			writeError(w, http.StatusBadRequest, "rule_ids must not contain duplicates.")
			return
		}
		seen[ruleID] = struct{}{}
	}

	startNDJSON(w)
	ctx, cancel := a.operationContext(r.Context())
	defer cancel()
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	emit := func(event CleanEvent) error {
		if event.Type == "" {
			return errors.New("dashboard: clean event type is required")
		}
		if err := encoder.Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	if err := a.service.Clean(ctx, request, emit); err != nil && ctx.Err() == nil {
		_ = emit(CleanEvent{Type: "clean.issue", Issue: &CleanIssue{
			Message: "Cleanup stopped before it could finish.",
		}})
	}
}

func (a *appHandler) operationContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(requestContext)
	stop := context.AfterFunc(a.baseContext, cancel)
	if a.baseContext.Err() != nil {
		cancel()
	}
	return ctx, func() {
		stop()
		cancel()
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("Request body must be one valid JSON object with only supported fields.")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("Request body must contain only one JSON object.")
	}
	return nil
}

func serveEmbedded(w http.ResponseWriter, r *http.Request, name, contentType string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	content, err := staticFiles.ReadFile(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
}

func startNDJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Cache-Control", "no-store")
		headers.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'; font-src 'none'; media-src 'none'; manifest-src 'none'; worker-src 'none'; require-trusted-types-for 'script'; trusted-types 'none'")
		headers.Set("Cross-Origin-Embedder-Policy", "require-corp")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")
		headers.Set("Origin-Agent-Cluster", "?1")
		headers.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(), payment=(), usb=()")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("X-Permitted-Cross-Domain-Policies", "none")
		next.ServeHTTP(w, r)
	})
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r.RemoteAddr) || !isLoopbackHost(r.Host) {
			writeError(w, http.StatusForbidden, "The dashboard is available only from this Mac.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOriginRequest(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	if err != nil || parsed.Scheme != expectedScheme || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host) && isLoopbackHost(parsed.Host)
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	return loopbackNameOrIP(host)
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	return err == nil && loopbackNameOrIP(host)
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	} else if strings.Contains(hostport, ":") {
		// An IPv6 host with no port is valid only when it is bracketed.
		if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
		} else {
			return false
		}
	}
	return loopbackNameOrIP(host)
}

func loopbackNameOrIP(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validProfile(profile string) bool {
	return profile == ProfileSafe || profile == ProfileBalanced || profile == ProfileReview
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '.' || character == '_' || character == ':' || character == '-') {
			continue
		}
		return false
	}
	return true
}

func isJSON(contentType string) bool {
	mediaType := strings.TrimSpace(strings.Split(contentType, ";")[0])
	return strings.EqualFold(mediaType, "application/json")
}
