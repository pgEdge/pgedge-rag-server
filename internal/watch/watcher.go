//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package watch detects changes to a set of files, including files
// delivered by atomic replacement (e.g. an orchestrator projecting a
// mounted secret or ConfigMap) rather than an in-place write.
//
// Tools that swap a file atomically typically do so by writing the new
// content elsewhere and renaming a symlink to point at it (Kubernetes'
// kubelet does exactly this for mounted Secrets/ConfigMaps: it retargets
// a hidden "..data" symlink and removes the old target directory). The
// visible file path a caller cares about is never itself written to —
// only the hidden symlink changes — so a watch on that exact path sees
// nothing. Watching the file's parent directory instead, and reacting to
// any change there rather than filtering by which name changed, catches
// this correctly. See issue #30.
package watch

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce is how long the Watcher waits after the last relevant
// filesystem event before invoking its callback. An atomic swap
// typically produces several events in quick succession (remove old
// target, create/rename new one); debouncing collapses these into one
// callback invocation instead of several redundant ones.
const DefaultDebounce = 500 * time.Millisecond

// Watcher watches the parent directories of a set of files and invokes
// onChange, debounced, whenever anything changes in one of those
// directories.
type Watcher struct {
	fsw      *fsnotify.Watcher
	debounce time.Duration
	onChange func()
	logger   *slog.Logger

	// reloadTrigger hands off debounced change notifications to a
	// separate worker goroutine (see reloadWorker) so that a slow
	// onChange (e.g. rebuilding pipelines) never blocks this package's
	// event loop from draining fsnotify events/errors or observing
	// context cancellation. Buffered to 1: a pending-but-not-yet-picked-up
	// notification coalesces with any further ones that arrive while
	// onChange is still running, so a burst during a slow reload
	// produces at most one more reload rather than one per event.
	reloadTrigger chan struct{}
}

// New creates a Watcher for the given file paths. Directories are
// deduplicated, so multiple watched files in the same directory share a
// single underlying watch. A path whose file doesn't exist yet is still
// watched via its parent directory (as long as the directory itself
// exists), so the watcher picks up the file once something creates it.
func New(paths []string, debounce time.Duration, onChange func(), logger *slog.Logger) (*Watcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if debounce <= 0 {
		debounce = DefaultDebounce
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dirs := make(map[string]bool)
	for _, p := range paths {
		dirs[filepath.Dir(p)] = true
	}
	for dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			_ = fsw.Close()
			return nil, err
		}
	}

	return &Watcher{
		fsw:           fsw,
		debounce:      debounce,
		onChange:      onChange,
		logger:        logger,
		reloadTrigger: make(chan struct{}, 1),
	}, nil
}

// Start runs the watch loop until ctx is canceled. Intended to be run in
// its own goroutine.
func (w *Watcher) Start(ctx context.Context) {
	// Scope the worker to this Start invocation: cancelling workerCtx
	// when Start returns (whether from ctx cancellation or the fsnotify
	// channels closing on Close) guarantees the worker cannot outlive the
	// event loop and fire a reload during shutdown.
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go w.reloadWorker(workerCtx)

	var timer *time.Timer
	var timerC <-chan time.Time

	resetDebounce := func() {
		stopTimer(timer)
		timer = time.NewTimer(w.debounce)
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.logger.Debug("watched directory changed",
				"path", event.Name, "op", event.Op.String())
			resetDebounce()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.logger.Error("file watcher error", "error", err)

		case <-timerC:
			timerC = nil
			w.triggerReload()
		}
	}
}

// stopTimer stops t if it was started. A no-op for a nil timer, which
// happens the first time Start's debounce case runs.
func stopTimer(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}

// triggerReload hands a debounced change notification to reloadWorker
// without blocking. If one is already pending or being processed, this
// is a no-op: reloadWorker only dequeues once it starts the next
// onChange call, so the pending notification already covers this change.
func (w *Watcher) triggerReload() {
	select {
	case w.reloadTrigger <- struct{}{}:
	default:
	}
}

// reloadWorker serializes onChange invocations on their own goroutine,
// so a slow onChange never blocks Start's event loop. It runs until ctx
// is canceled.
func (w *Watcher) reloadWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.reloadTrigger:
			// A trigger and ctx cancellation can both be ready at once;
			// select picks at random, so recheck before running onChange
			// to avoid a reload firing during shutdown.
			if ctx.Err() != nil {
				return
			}
			w.onChange()
		}
	}
}

// Close stops the watcher and releases its underlying resources.
func (w *Watcher) Close() error {
	return w.fsw.Close()
}
