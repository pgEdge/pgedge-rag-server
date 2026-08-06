//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
)

// refusingBackend is a SearchBackend that refuses every query the way
// the database layer does when no identity is present.
type refusingBackend struct {
	vectorCalls int
}

func (b *refusingBackend) VectorSearch(
	ctx context.Context,
	embedding []float32,
	table config.TableSource,
	topN int,
	filter *config.Filter,
	minSimilarity *float64,
) ([]database.SearchResult, error) {
	b.vectorCalls++
	return nil, identity.ErrRequired
}

func (b *refusingBackend) FetchDocuments(
	ctx context.Context,
	table config.TableSource,
	filter *config.Filter,
	maxDocuments int,
) (map[string]string, error) {
	return nil, identity.ErrRequired
}

// partiallyRefusingBackend lets the vector arm succeed and refuses the
// keyword arm, which is what a deployment sees if only one of the two
// corpus reads loses its identity.
type partiallyRefusingBackend struct {
	fetchCalls int
}

func (b *partiallyRefusingBackend) VectorSearch(
	ctx context.Context,
	embedding []float32,
	table config.TableSource,
	topN int,
	filter *config.Filter,
	minSimilarity *float64,
) ([]database.SearchResult, error) {
	return []database.SearchResult{{ID: "a1", Content: "alice", Score: 0.9}}, nil
}

func (b *partiallyRefusingBackend) FetchDocuments(
	ctx context.Context,
	table config.TableSource,
	filter *config.Filter,
	maxDocuments int,
) (map[string]string, error) {
	b.fetchCalls++
	return nil, identity.ErrRequired
}

// identityPipelineConfig returns a pipeline with two tables, so the
// "stops at the first table" property can be observed.
func identityPipelineConfig() *config.Pipeline {
	hybrid := false
	return &config.Pipeline{
		Name: "docs",
		Tables: []config.TableSource{
			{Table: "chunks_a", TextColumn: "content", VectorColumn: "embedding"},
			{Table: "chunks_b", TextColumn: "content", VectorColumn: "embedding"},
		},
		Search: config.SearchConfig{HybridEnabled: &hybrid},
	}
}

// TestSearch_PropagatesIdentityRefusal checks that a refusal for want of
// an identity reaches the caller as a refusal.
//
// The orchestrator's ordinary behaviour is to log a failing table and
// carry on, and to answer "No relevant information found in the
// available documents" when nothing comes back. Applying that to an
// identity refusal would be the worst possible outcome: the caller would
// be told the corpus is empty rather than that they were not entitled to
// search it, and an operator watching for errors would see none.
func TestSearch_PropagatesIdentityRefusal(t *testing.T) {
	backend := &refusingBackend{}
	orch := NewOrchestrator(OrchestratorConfig{
		Pipeline: identityPipelineConfig(),
		DBPool:   backend,
		TopN:     5,
		Logger:   slog.New(slog.DiscardHandler),
	})

	_, err := orch.search(context.Background(), QueryRequest{Query: "hello"},
		[]float32{1, 0, 0}, 5)

	if !errors.Is(err, identity.ErrRequired) {
		t.Fatalf("search returned %v, want identity.ErrRequired", err)
	}

	// It must also stop rather than trying every table in turn: no
	// subsequent table can succeed, and each attempt is a wasted round
	// trip on a request that is already refused.
	if backend.vectorCalls != 1 {
		t.Errorf("search made %d vector search attempts after a refusal, want 1",
			backend.vectorCalls)
	}
}

// TestSearch_PropagatesIdentityRefusalFromTheKeywordArm covers the same
// property for the BM25 arm, which reads the corpus through a second
// query. Both arms go through the identity-bearing path, so both can
// refuse, and a refusal from either must not be degraded into a partial
// result.
func TestSearch_PropagatesIdentityRefusalFromTheKeywordArm(t *testing.T) {
	backend := &partiallyRefusingBackend{}
	cfg := identityPipelineConfig()
	hybrid := true
	cfg.Search.HybridEnabled = &hybrid

	orch := NewOrchestrator(OrchestratorConfig{
		Pipeline: cfg,
		DBPool:   backend,
		TopN:     5,
		Logger:   slog.New(slog.DiscardHandler),
	})

	_, err := orch.search(context.Background(), QueryRequest{Query: "hello"},
		[]float32{1, 0, 0}, 5)

	if !errors.Is(err, identity.ErrRequired) {
		t.Fatalf("search returned %v, want identity.ErrRequired", err)
	}
	if backend.fetchCalls != 1 {
		t.Errorf("search made %d keyword-corpus reads after a refusal, want 1",
			backend.fetchCalls)
	}
}

// TestExecute_DoesNotAnswerAnIdentityRefusal is the same property seen
// from the top of the pipeline: Execute must return the error rather
// than the "no relevant information" answer.
func TestExecute_DoesNotAnswerAnIdentityRefusal(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Pipeline:      identityPipelineConfig(),
		DBPool:        &refusingBackend{},
		EmbeddingProv: &MockEmbedder{},
		TopN:          5,
		Logger:        slog.New(slog.DiscardHandler),
	})

	resp, err := orch.Execute(context.Background(), QueryRequest{Query: "hello"})

	if !errors.Is(err, identity.ErrRequired) {
		t.Fatalf("Execute returned (%+v, %v), want identity.ErrRequired", resp, err)
	}
	if resp != nil {
		t.Errorf("Execute returned an answer alongside a refusal: %+v", resp)
	}
}

// TestExecuteStream_DoesNotAnswerAnIdentityRefusal covers the streaming
// path, where the same degradation would surface as a cheerful "no
// relevant information" chunk.
func TestExecuteStream_DoesNotAnswerAnIdentityRefusal(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Pipeline:      identityPipelineConfig(),
		DBPool:        &refusingBackend{},
		EmbeddingProv: &MockEmbedder{},
		TopN:          5,
		Logger:        slog.New(slog.DiscardHandler),
	})

	chunks, errs := orch.ExecuteStream(context.Background(),
		QueryRequest{Query: "hello"})

	for chunk := range chunks {
		t.Errorf("a refused stream produced a chunk: %+v", chunk)
	}
	if err := <-errs; !errors.Is(err, identity.ErrRequired) {
		t.Errorf("stream error = %v, want identity.ErrRequired", err)
	}
}

// stubVerifier reports a fixed set of enforcement problems.
type stubVerifier struct {
	problems []error
	calls    int
}

func (s *stubVerifier) VerifyEnforcement(
	ctx context.Context,
	tables []config.TableSource,
) []error {
	s.calls++
	return s.problems
}

// TestVerifyEnforcementPolicy covers what the manager does with the
// preflight's findings under each enforcement_check setting.
func TestVerifyEnforcementPolicy(t *testing.T) {
	problems := []error{
		errors.New("row-level security is not enabled on \"chunks_a\""),
		errors.New("no policy on \"chunks_b\" refers to request.jwt.claims"),
	}

	tests := []struct {
		name     string
		check    string
		problems []error
		wantErr  bool
		wantSaid []string
	}{
		{
			name:     "the default aborts startup on any finding",
			check:    "",
			problems: problems,
			wantErr:  true,
			wantSaid: []string{"chunks_a", "chunks_b"},
		},
		{
			name:     "error aborts startup on any finding",
			check:    config.EnforcementCheckError,
			problems: problems,
			wantErr:  true,
			wantSaid: []string{"chunks_a", "chunks_b"},
		},
		{
			name:     "warn serves anyway",
			check:    config.EnforcementCheckWarn,
			problems: problems,
			wantErr:  false,
		},
		{
			name:     "a clean preflight is not an error under any setting",
			check:    config.EnforcementCheckError,
			problems: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := &stubVerifier{problems: tt.problems}
			idCfg := config.IdentityConfig{Enabled: true, EnforcementCheck: tt.check}

			err := verifyEnforcement(context.Background(),
				slog.New(slog.DiscardHandler), idCfg, verifier, nil)

			if tt.wantErr && err == nil {
				t.Fatal("expected startup to be aborted, got no error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected startup to continue, got: %v", err)
			}
			if verifier.calls != 1 {
				t.Errorf("the preflight ran %d times, want exactly 1",
					verifier.calls)
			}

			// Every finding must be reported, not just the first: an
			// operator fixing one table at a time and restarting for each
			// is a needlessly bad experience.
			for _, want := range tt.wantSaid {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the abort message does not mention %q: %v", want, err)
				}
			}
		})
	}
}
