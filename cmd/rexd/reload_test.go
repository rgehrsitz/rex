package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/observability"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

func reloadArtifact(t *testing.T, target string) []byte {
	t.Helper()
	data, err := compiler.CompileBatch([]byte(`{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"` + target + `","value":true}]}]}`))
	require.NoError(t, err)
	return data
}

func reloadFixture(t *testing.T, stats durableStatsReader) (*rulesetReloader, *runtime.EngineManager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "active.bytecode")
	data := reloadArtifact(t, "old-output")
	require.NoError(t, os.WriteFile(path, data, 0o640))
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	engine, err := runtime.NewEngineFromBytes(data, memory, 0)
	require.NoError(t, err)
	manager, err := runtime.NewEngineManager(engine)
	require.NoError(t, err)
	config := &Config{
		BytecodeFile: path, PriorityThreshold: 0, BatchLimits: runtime.DefaultLimits(),
		MaxActionsPerEvaluation: runtime.DefaultMaxActionsPerEvaluation,
		ReloadInterval:          time.Second, ReloadHistoryDir: filepath.Join(dir, "history"), ReloadHistoryMaxFiles: 4,
	}
	reloader, err := newRulesetReloader(config, &RexDependencies{Store: memory, Engine: engine, Manager: manager}, observability.NewMetrics(), stats)
	require.NoError(t, err)
	return reloader, manager, path
}

func TestRulesetReloadRejectsInvalidCandidateAndKeepsActive(t *testing.T) {
	reloader, manager, path := reloadFixture(t, nil)
	active := manager.ActiveProgramID()
	require.NoError(t, os.WriteFile(path, []byte("not bytecode"), 0o640))
	_, err := reloader.reload(context.Background())
	require.ErrorContains(t, err, "validate reload candidate")
	require.Equal(t, active, manager.ActiveProgramID())
	programID, err := reloader.reload(context.Background())
	require.NoError(t, err)
	require.Empty(t, programID, "an unchanged invalid candidate should not be revalidated")
}

func TestRulesetReloadArchivesThenAtomicallyActivatesCandidate(t *testing.T) {
	reloader, manager, path := reloadFixture(t, nil)
	oldID := manager.ActiveProgramID()
	candidate := reloadArtifact(t, "new-output")
	require.NoError(t, os.WriteFile(path, candidate, 0o640))
	newID, err := reloader.reload(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, oldID, newID)
	require.Equal(t, newID, manager.ActiveProgramID())
	_, err = os.Stat(filepath.Join(reloader.config.ReloadHistoryDir, oldID+".bytecode"))
	require.NoError(t, err)
	archived, err := os.ReadFile(filepath.Join(reloader.config.ReloadHistoryDir, newID+".bytecode"))
	require.NoError(t, err)
	require.Equal(t, candidate, archived)
}

type reloadStats struct{ pending int64 }

func (s *reloadStats) Stats(context.Context) (store.DurableStats, error) {
	return store.DurableStats{Pending: s.pending}, nil
}

func TestRulesetReloadWaitsForPendingDurableWork(t *testing.T) {
	stats := &reloadStats{pending: 1}
	reloader, manager, path := reloadFixture(t, stats)
	oldID := manager.ActiveProgramID()
	require.NoError(t, os.WriteFile(path, reloadArtifact(t, "new-output"), 0o640))
	_, err := reloader.reload(context.Background())
	require.ErrorIs(t, err, errReloadDeferred)
	require.Equal(t, oldID, manager.ActiveProgramID())
	stats.pending = 0
	newID, err := reloader.reload(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, oldID, newID)
}

func TestRulesetReloadRetriesAfterHistoryCapacityIsFreed(t *testing.T) {
	reloader, manager, path := reloadFixture(t, nil)
	oldID := manager.ActiveProgramID()
	reloader.config.ReloadHistoryMaxFiles = 1
	require.NoError(t, os.WriteFile(path, reloadArtifact(t, "new-output"), 0o640))
	_, err := reloader.reload(context.Background())
	require.ErrorContains(t, err, "history is full")
	require.Equal(t, oldID, manager.ActiveProgramID())
	require.NoError(t, os.Remove(filepath.Join(reloader.config.ReloadHistoryDir, oldID+".bytecode")))
	newID, err := reloader.reload(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, oldID, newID)
}

func TestRulesetReloadRejectsMisnamedHistory(t *testing.T) {
	reloader, _, _ := reloadFixture(t, nil)
	require.NoError(t, os.WriteFile(filepath.Join(reloader.config.ReloadHistoryDir, "wrong.bytecode"), reloadArtifact(t, "other-output"), 0o640))
	err := reloader.loadHistory()
	require.ErrorContains(t, err, "does not match its program digest")
}

type interleavingQueue struct {
	memory        *store.MemoryStore
	event         store.DurableEvent
	entered       chan struct{}
	release       chan struct{}
	mu            sync.Mutex
	pinned        string
	attempts      int64
	firstAttempt  bool
	beginPrograms []string
}

func (q *interleavingQueue) Next(context.Context) (store.DurableEvent, error) { return q.event, nil }
func (q *interleavingQueue) PinnedProgram(context.Context, string) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pinned, nil
}
func (q *interleavingQueue) Begin(_ context.Context, _ store.DurableEvent, programID string) (store.JournalStatus, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pinned == "" {
		q.pinned = programID
	}
	q.attempts++
	q.beginPrograms = append(q.beginPrograms, programID)
	return store.JournalStatus{Attempts: q.attempts}, nil
}
func (q *interleavingQueue) ApplyInput(ctx context.Context, _ string, _ string, facts map[string]interface{}) error {
	q.mu.Lock()
	first := q.firstAttempt
	if first {
		q.firstAttempt = false
		close(q.entered)
	}
	q.mu.Unlock()
	if first {
		select {
		case <-q.release:
			return errors.New("force pinned retry")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for key, value := range facts {
		if err := q.memory.SetFactContext(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}
func (q *interleavingQueue) Acknowledge(context.Context, string) error { return nil }
func (q *interleavingQueue) Complete(context.Context, string) error    { return nil }
func (q *interleavingQueue) DeadLetterEvent(context.Context, store.DurableEvent, string, int64, error) error {
	return nil
}
func (q *interleavingQueue) MaxAttempts() int64 { return 3 }

type interleavingStats struct {
	once      sync.Once
	manager   *runtime.EngineManager
	queue     *interleavingQueue
	processed chan error
}

func (s *interleavingStats) Stats(ctx context.Context) (store.DurableStats, error) {
	s.once.Do(func() {
		go func() {
			_, err := s.manager.ProcessNextDurable(ctx, s.queue)
			s.processed <- err
		}()
	})
	select {
	case <-s.queue.entered:
		return store.DurableStats{Lag: 7}, nil
	case <-ctx.Done():
		return store.DurableStats{}, ctx.Err()
	}
}

func TestRulesetReloadPreservesProgramForEventEnteringAfterPendingCheck(t *testing.T) {
	reloader, manager, path := reloadFixture(t, nil)
	oldID := manager.ActiveProgramID()
	queue := &interleavingQueue{
		memory: reloader.store.(*store.MemoryStore), event: store.DurableEvent{ID: "1-0", Payload: `{"a":1}`},
		entered: make(chan struct{}), release: make(chan struct{}), firstAttempt: true,
	}
	stats := &interleavingStats{manager: manager, queue: queue, processed: make(chan error, 1)}
	reloader.stats = stats
	require.NoError(t, os.WriteFile(path, reloadArtifact(t, "new-output"), 0o640))
	reloadDone := make(chan struct {
		programID string
		err       error
	}, 1)
	go func() {
		programID, err := reloader.reload(context.Background())
		reloadDone <- struct {
			programID string
			err       error
		}{programID: programID, err: err}
	}()
	<-queue.entered
	select {
	case <-reloadDone:
		t.Fatal("reload completed while the late event still held its old program")
	case <-time.After(25 * time.Millisecond):
	}
	close(queue.release)
	require.ErrorContains(t, <-stats.processed, "force pinned retry")
	reloaded := <-reloadDone
	require.NoError(t, reloaded.err)
	require.NotEqual(t, oldID, reloaded.programID)
	result, err := manager.ProcessNextDurable(context.Background(), queue)
	require.NoError(t, err)
	require.True(t, result.Processed)
	require.Equal(t, []string{oldID, oldID}, queue.beginPrograms)
}

func TestRulesetReloadCleansAbandonedTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".ruleset-abandoned.tmp")
	require.NoError(t, os.WriteFile(stale, []byte("partial"), 0o640))
	require.NoError(t, cleanupReloadTemps(dir))
	_, err := os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist)
}
