//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func waitForChange(t *testing.T, changed *atomic.Bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if changed.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for onChange to fire")
}

// TestWatcher_DirectWrite is the simple baseline case: an in-place write
// to the exact watched file must be detected.
func TestWatcher_DirectWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	var changed atomic.Bool
	w, err := New([]string{path}, 50*time.Millisecond, func() { changed.Store(true) }, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(50 * time.Millisecond) // let the watcher settle before writing
	if err := os.WriteFile(path, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}

	waitForChange(t, &changed, 2*time.Second)
}

// TestWatcher_AtomicSymlinkReplacement is the regression test for issue
// #30: it reproduces the exact mechanism Kubernetes uses to deliver a
// mounted Secret/ConfigMap update — a hidden "..data" symlink is
// re-targeted via rename, and the old target directory is removed. The
// visible file ("apikey") is a symlink to "..data/apikey" and is never
// itself written to; only "..data" changes. A watch on the exact file
// path would see nothing. This must still be detected because the
// watcher watches the parent directory instead.
func TestWatcher_AtomicSymlinkReplacement(t *testing.T) {
	dir := t.TempDir()

	// Set up the initial k8s-style layout:
	//   dir/..data_v1/apikey  (real content)
	//   dir/..data -> ..data_v1
	//   dir/apikey -> ..data/apikey   <- this is the path callers use
	dataV1 := filepath.Join(dir, "..data_v1")
	if err := os.Mkdir(dataV1, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataV1, "apikey"), []byte("old-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataLink := filepath.Join(dir, "..data")
	if err := os.Symlink("..data_v1", dataLink); err != nil {
		t.Fatal(err)
	}
	visiblePath := filepath.Join(dir, "apikey")
	if err := os.Symlink(filepath.Join("..data", "apikey"), visiblePath); err != nil {
		t.Fatal(err)
	}

	// Sanity: reading through the symlink chain gets the original content.
	before, err := os.ReadFile(visiblePath)
	if err != nil {
		t.Fatalf("failed to read through initial symlink chain: %v", err)
	}
	if string(before) != "old-secret" {
		t.Fatalf("expected 'old-secret', got %q", before)
	}

	var changed atomic.Bool
	w, err := New([]string{visiblePath}, 50*time.Millisecond, func() { changed.Store(true) }, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(50 * time.Millisecond) // let the watcher settle before the swap

	// Simulate the atomic update: write new content under a new target
	// directory, atomically rename a new symlink over "..data", then
	// remove the old target. The visible "apikey" symlink itself is
	// never touched — only "..data" changes, exactly as kubelet does it.
	dataV2 := filepath.Join(dir, "..data_v2")
	if err := os.Mkdir(dataV2, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataV2, "apikey"), []byte("new-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpLink := filepath.Join(dir, "..data_tmp")
	if err := os.Symlink("..data_v2", tmpLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpLink, dataLink); err != nil { // atomic swap
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataV1); err != nil {
		t.Fatal(err)
	}

	waitForChange(t, &changed, 2*time.Second)

	// The whole point: re-reading the SAME visible path now returns the
	// new content, because the underlying "..data" target changed.
	after, err := os.ReadFile(visiblePath)
	if err != nil {
		t.Fatalf("failed to read through post-swap symlink chain: %v", err)
	}
	if string(after) != "new-secret" {
		t.Fatalf("expected 'new-secret' after atomic swap, got %q", after)
	}
}

// TestWatcher_Debounce verifies that several rapid changes collapse into
// a single onChange invocation, matching how an atomic swap produces
// multiple filesystem events for one logical change.
func TestWatcher_Debounce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	var callCount atomic.Int32
	w, err := New([]string{path}, 300*time.Millisecond, func() { callCount.Add(1) }, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(path, []byte("v"+string(rune('2'+i))), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond) // faster than the 300ms debounce
	}

	time.Sleep(600 * time.Millisecond) // let the debounce timer fire once

	if got := callCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 debounced callback for 5 rapid writes, got %d", got)
	}
}
