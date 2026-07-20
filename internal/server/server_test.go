//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// mockPipelineManager implements PipelineManager for testing.
type mockPipelineManager struct {
	pipelines map[string]*mockPipelineInfo
}

type mockPipelineInfo struct {
	name        string
	description string
	// executor, when non-nil, is returned by GetExecutor for this
	// pipeline. Nil means GetExecutor returns a nil QueryExecutor,
	// matching the "nil pipeline" defensive-check tests below — see
	// issue #37.
	executor pipeline.QueryExecutor
}

func newMockPipelineManager() *mockPipelineManager {
	return &mockPipelineManager{
		pipelines: map[string]*mockPipelineInfo{
			"test-pipeline": {
				name:        "test-pipeline",
				description: "A test pipeline",
			},
		},
	}
}

func (m *mockPipelineManager) List() []pipeline.Info {
	infos := make([]pipeline.Info, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		infos = append(infos, pipeline.Info{
			Name:        p.name,
			Description: p.description,
		})
	}
	return infos
}

func (m *mockPipelineManager) GetExecutor(name string) (pipeline.QueryExecutor, error) {
	info, ok := m.pipelines[name]
	if !ok {
		return nil, pipeline.ErrPipelineNotFound
	}
	// info.executor is nil unless a test explicitly configures one;
	// returning it directly (rather than wrapping it) keeps that a
	// genuine nil interface, not a nil-pointer-in-interface footgun.
	return info.executor, nil
}

func (m *mockPipelineManager) Close() error {
	return nil
}

// mockQueryExecutor implements pipeline.QueryExecutor for server tests
// that need to control execution behavior (e.g. simulate a hang past
// the request timeout, or a retrieval error) without a real pipeline —
// see issue #37.
type mockQueryExecutor struct {
	ExecuteWithOptionsFunc func(
		ctx context.Context, req pipeline.QueryRequest,
	) (*pipeline.QueryResponse, error)
	ExecuteStreamWithOptionsFunc func(
		ctx context.Context, req pipeline.QueryRequest,
	) (<-chan pipeline.StreamChunk, <-chan error)
}

func (m *mockQueryExecutor) ExecuteWithOptions(
	ctx context.Context, req pipeline.QueryRequest,
) (*pipeline.QueryResponse, error) {
	if m.ExecuteWithOptionsFunc != nil {
		return m.ExecuteWithOptionsFunc(ctx, req)
	}
	return &pipeline.QueryResponse{Answer: "mock answer"}, nil
}

func (m *mockQueryExecutor) ExecuteStreamWithOptions(
	ctx context.Context, req pipeline.QueryRequest,
) (<-chan pipeline.StreamChunk, <-chan error) {
	if m.ExecuteStreamWithOptionsFunc != nil {
		return m.ExecuteStreamWithOptionsFunc(ctx, req)
	}
	chunkChan := make(chan pipeline.StreamChunk)
	errChan := make(chan error, 1)
	close(chunkChan)
	close(errChan)
	return chunkChan, errChan
}

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			ListenAddress: "127.0.0.1",
			Port:          8080,
		},
		Pipelines: []config.Pipeline{
			{
				Name:        "test-pipeline",
				Description: "A test pipeline",
			},
		},
	}
}

func testServer() *Server {
	cfg := testConfig()
	pm := newMockPipelineManager()
	return New(cfg, pm, nil)
}

func TestHealthEndpoint(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status 'healthy', got '%s'", resp.Status)
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodPost, "/v1/health", nil)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestListPipelinesEndpoint(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/pipelines", nil)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp PipelinesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Pipelines) != 1 {
		t.Errorf("expected 1 pipeline, got %d", len(resp.Pipelines))
	}

	if resp.Pipelines[0].Name != "test-pipeline" {
		t.Errorf("expected pipeline name 'test-pipeline', got '%s'",
			resp.Pipelines[0].Name)
	}
}

func TestPipelineEndpoint_NotFound(t *testing.T) {
	srv := testServer()

	body := bytes.NewBufferString(`{"query": "test query"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/nonexistent", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestPipelineEndpoint_EmptyQuery(t *testing.T) {
	srv := testServer()

	body := bytes.NewBufferString(`{"query": ""}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestPipelineEndpoint_InvalidJSON(t *testing.T) {
	srv := testServer()

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestPipelineEndpoint_NilPipeline(t *testing.T) {
	// When mock returns nil pipeline, we should get an error
	srv := testServer()

	body := bytes.NewBufferString(`{"query": "test query"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	// With mock returning nil pipeline, handler should return internal error
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestPipelineEndpoint_Streaming_NilPipeline(t *testing.T) {
	srv := testServer()

	body := bytes.NewBufferString(`{"query": "test query", "stream": true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	// With mock returning nil pipeline, we get internal error
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// TestPipelineEndpoint_NonStreamingTimeout is a regression test for
// issue #37: it drives the actual handler-level timeout behavior added
// in #33 (context.WithTimeout wrapping ExecuteWithOptions) through a
// fake QueryExecutor that hangs until the request's own timeout fires,
// rather than relying on a real slow provider. Previously this could
// only be verified by hand against a live, artificially slow backend.
func TestPipelineEndpoint_NonStreamingTimeout(t *testing.T) {
	pm := newMockPipelineManager()
	pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
		ExecuteWithOptionsFunc: func(ctx context.Context, req pipeline.QueryRequest) (*pipeline.QueryResponse, error) {
			<-ctx.Done() // hang until the server's own request timeout fires
			return nil, ctx.Err()
		},
	}
	srv := New(testConfig(), pm, nil)
	srv.requestTimeout = 50 * time.Millisecond

	body := bytes.NewBufferString(`{"query": "test query"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, w.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != "REQUEST_TIMEOUT" {
		t.Errorf("expected error code REQUEST_TIMEOUT, got %q", resp.Error.Code)
	}
}

// TestPipelineEndpoint_StreamingTimeout is a regression test for issue
// #37: it drives the streaming timeout path added in #33 through a
// fake QueryExecutor whose stream channels never receive anything,
// forcing the handler's ctx.Done() case to fire once the request
// timeout elapses. Confirms the client gets an SSE "error" event
// followed by "done", with no chunks in between — previously only
// verified by hand against a live, artificially slow backend.
func TestPipelineEndpoint_StreamingTimeout(t *testing.T) {
	pm := newMockPipelineManager()
	pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
		ExecuteStreamWithOptionsFunc: func(ctx context.Context, req pipeline.QueryRequest) (<-chan pipeline.StreamChunk, <-chan error) {
			// Channels that never receive anything: the handler's
			// ctx.Done() case is the only way this select resolves.
			return make(chan pipeline.StreamChunk), make(chan error, 1)
		},
	}
	srv := New(testConfig(), pm, nil)
	srv.requestTimeout = 50 * time.Millisecond

	body := bytes.NewBufferString(`{"query": "test query", "stream": true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	got := w.Body.String()
	errIdx := strings.Index(got, `"type":"error"`)
	doneIdx := strings.Index(got, `"type":"done"`)
	if errIdx < 0 {
		t.Fatalf("expected an SSE error event, got body: %s", got)
	}
	if doneIdx < 0 {
		t.Fatalf("expected an SSE done event, got body: %s", got)
	}
	if errIdx > doneIdx {
		t.Errorf("expected the error event before the done event, got body: %s", got)
	}
	if !strings.Contains(got, "request took too long to process") {
		t.Errorf("expected the timeout message in the error event, got body: %s", got)
	}
}

func TestSSEFormat(t *testing.T) {
	// Test that SSE events are properly formatted
	event := pipeline.StreamEvent{
		Type:    "chunk",
		Content: "Hello",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	sseData := "data: " + string(data) + "\n\n"

	if !strings.HasPrefix(sseData, "data: ") {
		t.Error("SSE data should start with 'data: '")
	}

	if !strings.HasSuffix(sseData, "\n\n") {
		t.Error("SSE data should end with '\\n\\n'")
	}
}

func TestOpenAPIEndpoint(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/openapi.json", nil)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check Content-Type
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", ct)
	}

	// Check RFC 8631 Link header
	link := w.Header().Get("Link")
	if link == "" {
		t.Error("expected Link header for RFC 8631 API discovery")
	}
	if !strings.Contains(link, `rel="service-desc"`) {
		t.Errorf("Link header should contain rel=\"service-desc\", got '%s'", link)
	}

	// Verify response is valid OpenAPI spec
	var spec map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&spec); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check required OpenAPI fields
	if spec["openapi"] == nil {
		t.Error("OpenAPI spec missing 'openapi' field")
	}
	if spec["info"] == nil {
		t.Error("OpenAPI spec missing 'info' field")
	}
	if spec["paths"] == nil {
		t.Error("OpenAPI spec missing 'paths' field")
	}
	if spec["components"] == nil {
		t.Error("OpenAPI spec missing 'components' field")
	}

	// Check version
	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected OpenAPI version '3.0.3', got '%v'", spec["openapi"])
	}
}

// TestUnknownRoute_ReturnsJSON404 is a regression test for issue #31:
// requests to unregistered paths must get a structured JSON error, not
// net/http's default plain-text "404 page not found". Uses the full
// middleware chain (applyMiddleware), not the raw mux, since that's
// where routingMiddleware intercepts the mismatch.
func TestUnknownRoute_ReturnsJSON404(t *testing.T) {
	srv := testServer()
	handler := srv.applyMiddleware(srv.mux)

	req := httptest.NewRequest(http.MethodGet, "/this-route-does-not-exist", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("expected error code NOT_FOUND, got %q", resp.Error.Code)
	}
}

// TestMethodNotAllowed_ThroughMiddleware_ReturnsJSON405 is a regression
// test for issue #31: a registered path hit with the wrong method must
// get a structured JSON error with a correct Allow header, not net/http's
// default plain-text "405 Method Not Allowed". net/http's ServeMux
// intercepts this before any handler runs, so this only exercises the
// fix when going through the full middleware chain (applyMiddleware),
// unlike TestHealthEndpoint_MethodNotAllowed above which hits the raw
// mux and observes net/http's own (also-405, but plain-text) response.
func TestMethodNotAllowed_ThroughMiddleware_ReturnsJSON405(t *testing.T) {
	srv := testServer()
	handler := srv.applyMiddleware(srv.mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("expected Allow header to contain GET, got %q", allow)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if resp.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("expected error code METHOD_NOT_ALLOWED, got %q", resp.Error.Code)
	}
}

// TestAllowedMethods_ReflectsRegisteredRoutes checks the mux-probing
// helper directly against this server's actual registered routes.
func TestAllowedMethods_ReflectsRegisteredRoutes(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodDelete, "/v1/pipelines/some-name", nil)
	allowed := srv.allowedMethods(req)

	if len(allowed) != 1 || allowed[0] != http.MethodPost {
		t.Errorf("expected only POST allowed for /v1/pipelines/{name}, got %v", allowed)
	}

	req2 := httptest.NewRequest(http.MethodDelete, "/no-such-path", nil)
	if allowed2 := srv.allowedMethods(req2); len(allowed2) != 0 {
		t.Errorf("expected no allowed methods for an unregistered path, got %v", allowed2)
	}
}

// TestAllowedMethods_IncludesImplicitHEAD verifies that a GET-registered
// route reports HEAD as allowed too, matching net/http.ServeMux's own
// behavior of implicitly serving HEAD wherever GET is registered — even
// though only GET appears in routes.go.
func TestAllowedMethods_IncludesImplicitHEAD(t *testing.T) {
	srv := testServer()

	req := httptest.NewRequest(http.MethodDelete, "/v1/health", nil)
	allowed := srv.allowedMethods(req)

	hasGet, hasHead := false, false
	for _, m := range allowed {
		if m == http.MethodGet {
			hasGet = true
		}
		if m == http.MethodHead {
			hasHead = true
		}
	}
	if !hasGet || !hasHead {
		t.Errorf("expected both GET and HEAD allowed for /v1/health, got %v", allowed)
	}
}

// TestPipelineEndpoint_RequestTooLarge is a regression test for issue
// #31: a request body over maxRequestBodyBytes must be rejected with a
// structured JSON 413, not silently accepted (previously there was no
// size limit at all) or surfaced as a generic 400.
func TestPipelineEndpoint_RequestTooLarge(t *testing.T) {
	srv := testServer()

	oversized := strings.Repeat("x", maxRequestBodyBytes+1)
	body := `{"query":"` + oversized + `"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if resp.Error.Code != "REQUEST_TOO_LARGE" {
		t.Errorf("expected error code REQUEST_TOO_LARGE, got %q", resp.Error.Code)
	}
}

func TestRFC8631LinkHeader(t *testing.T) {
	srv := testServer()

	// Test that Link header is present on all API responses
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/health"},
		{http.MethodGet, "/v1/pipelines"},
		{http.MethodGet, "/v1/openapi.json"},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)

		link := w.Header().Get("Link")
		if link == "" {
			t.Errorf("%s %s: missing Link header", ep.method, ep.path)
			continue
		}
		if !strings.Contains(link, "</v1/openapi.json>") {
			t.Errorf("%s %s: Link header should reference /v1/openapi.json", ep.method, ep.path)
		}
		if !strings.Contains(link, `rel="service-desc"`) {
			t.Errorf("%s %s: Link header should have rel=\"service-desc\"", ep.method, ep.path)
		}
	}
}

// TestIsRequestTimeout_DeadlineExceeded is a regression test for issue
// #31: isRequestTimeout must report true when a context's own deadline
// elapsed (the server's request timeout firing).
func TestIsRequestTimeout_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	if !isRequestTimeout(ctx) {
		t.Errorf("expected isRequestTimeout to be true after the deadline elapsed, ctx.Err()=%v", ctx.Err())
	}
}

// TestIsRequestTimeout_Canceled verifies isRequestTimeout distinguishes
// its own timeout from an ordinary cancellation (e.g. the client
// disconnecting), which must NOT be reported as a timeout.
func TestIsRequestTimeout_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if isRequestTimeout(ctx) {
		t.Error("expected isRequestTimeout to be false for a plain cancellation, not a deadline")
	}
}

// TestIsRequestTimeout_StillRunning verifies isRequestTimeout is false
// while a context is still active.
func TestIsRequestTimeout_StillRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	if isRequestTimeout(ctx) {
		t.Error("expected isRequestTimeout to be false for a context that hasn't finished")
	}
}
