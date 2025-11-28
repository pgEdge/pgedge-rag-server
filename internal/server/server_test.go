//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func (m *mockPipelineManager) Get(name string) (*pipeline.Pipeline, error) {
	if _, ok := m.pipelines[name]; !ok {
		return nil, pipeline.ErrPipelineNotFound
	}
	// Return nil pipeline - tests that need a real pipeline should use
	// a different approach
	return nil, nil
}

func (m *mockPipelineManager) Close() error {
	return nil
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
