//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("https://api.example.com")
	if c.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL https://api.example.com, got %s",
			c.baseURL)
	}
	if c.authFn != nil {
		t.Error("expected nil authFn by default")
	}
	if c.headers != nil {
		t.Error("expected nil headers by default")
	}
}

func TestClient_Do_GET(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.URL.Path != "/test" {
				t.Errorf("expected /test, got %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	)
	defer server.Close()

	c := NewClient(server.URL)
	resp, err := c.Do(context.Background(), http.MethodGet,
		"/test", nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("expected ok, got %s", string(body))
	}
}

func TestClient_Do_POST_WithBody(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected application/json Content-Type, got %s",
					r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"key":"value"}` {
				t.Errorf("unexpected body: %s", string(body))
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	c := NewClient(server.URL)
	resp, err := c.Do(context.Background(), http.MethodPost,
		"/test", []byte(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestBearerAuth(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-key" {
				t.Errorf("expected 'Bearer test-key', got '%s'",
					auth)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	c := NewClient(server.URL, WithAuth(BearerAuth("test-key")))
	resp, err := c.Do(context.Background(), http.MethodGet,
		"/test", nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestHeaderAuth(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("x-api-key")
			if key != "test-key" {
				t.Errorf("expected 'test-key', got '%s'", key)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	c := NewClient(server.URL,
		WithAuth(HeaderAuth("x-api-key", "test-key")))
	resp, err := c.Do(context.Background(), http.MethodGet,
		"/test", nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestQueryParamAuth(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.URL.Query().Get("key")
			if key != "test-key" {
				t.Errorf("expected 'test-key', got '%s'", key)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	c := NewClient(server.URL,
		WithAuth(QueryParamAuth("key", "test-key")))
	resp, err := c.Do(context.Background(), http.MethodGet,
		"/test", nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestNoAuth(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("expected no Authorization header")
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	c := NewClient(server.URL, WithAuth(NoAuth()))
	resp, err := c.Do(context.Background(), http.MethodGet,
		"/test", nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	_ = resp.Body.Close()
}
