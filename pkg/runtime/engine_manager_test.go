package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

func managerEngine(t *testing.T, backend store.ContextStore, target string) *Engine {
	t.Helper()
	artifact, err := compiler.CompileBatch([]byte(`{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"` + target + `","value":true}]}]}`))
	require.NoError(t, err)
	engine, err := NewEngineFromBytes(artifact, backend, 0)
	require.NoError(t, err)
	return engine
}

func TestEngineManagerUsesPinnedHistoricalProgram(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	oldEngine := managerEngine(t, memory, "old-output")
	newEngine := managerEngine(t, memory, "new-output")
	manager, err := NewEngineManager(newEngine)
	require.NoError(t, err)
	require.NoError(t, manager.Register(oldEngine))
	queue := &durableQueueProbe{
		event:  store.DurableEvent{ID: "1-0", Payload: `{"a":1}`, Recovered: true},
		pinned: oldEngine.ProgramID(), maxAttempts: 3,
	}

	result, err := manager.ProcessNextDurable(context.Background(), queue)
	require.NoError(t, err)
	require.True(t, result.Processed)
	require.Equal(t, oldEngine.ProgramID(), queue.begunProgram)
	facts, err := memory.Snapshot()
	require.NoError(t, err)
	require.Equal(t, true, facts["old-output"])
	require.NotContains(t, facts, "new-output")
}

type blockingCommitStore struct {
	*store.MemoryStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingCommitStore) Commit(ctx context.Context, request store.CommitRequest) (store.CommitResult, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return store.CommitResult{}, ctx.Err()
	}
	return s.MemoryStore.Commit(ctx, request)
}

func TestEngineManagerSwapsOnlyAtEventBoundary(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	backend := &blockingCommitStore{MemoryStore: memory, entered: make(chan struct{}), release: make(chan struct{})}
	manager, err := NewEngineManager(managerEngine(t, backend, "old-output"))
	require.NoError(t, err)
	candidate := managerEngine(t, backend, "new-output")
	eventDone := make(chan error, 1)
	go func() {
		eventDone <- manager.ProcessBatchContext(context.Background(), map[string]interface{}{"a": float64(1)})
	}()
	<-backend.entered
	swapDone := make(chan error, 1)
	go func() { swapDone <- manager.Swap(candidate) }()
	select {
	case <-swapDone:
		t.Fatal("swap completed while an event was still in flight")
	case <-time.After(25 * time.Millisecond):
	}
	close(backend.release)
	require.NoError(t, <-eventDone)
	require.NoError(t, <-swapDone)
	require.Equal(t, candidate.ProgramID(), manager.ActiveProgramID())
}

func TestEngineManagerReleasesPrunedHistoricalPrograms(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	oldEngine := managerEngine(t, memory, "old-output")
	newEngine := managerEngine(t, memory, "new-output")
	manager, err := NewEngineManager(oldEngine)
	require.NoError(t, err)
	require.NoError(t, manager.Swap(newEngine))
	removed := manager.RetainPrograms(map[string]struct{}{newEngine.ProgramID(): {}})
	require.Equal(t, []string{oldEngine.ProgramID()}, removed)
	queue := &durableQueueProbe{
		event:  store.DurableEvent{ID: "2-0", Payload: `{"a":1}`, Recovered: true},
		pinned: oldEngine.ProgramID(), maxAttempts: 3,
	}
	_, err = manager.ProcessNextDurable(context.Background(), queue)
	require.ErrorIs(t, err, store.ErrDurableProgramMismatch)
}

func TestEngineManagerCleansTemporalStateWhenHistoryIsReleased(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	p, artifact := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	oldEngine, err := NewEngineFromBytes(artifact, memory, 0)
	require.NoError(t, err)
	clock := &testClock{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
	require.NoError(t, oldEngine.SetClock(clock))
	_, err = oldEngine.EvaluateBatch(context.Background(), map[string]interface{}{"hot": true})
	require.NoError(t, err)
	var trackerKey string
	for _, temporal := range p.temporal {
		trackerKey = temporal.key
	}
	state, err := memory.ReadSnapshot(context.Background(), []string{trackerKey})
	require.NoError(t, err)
	require.Equal(t, store.Present, state[trackerKey].State)

	manager, err := NewEngineManager(oldEngine)
	require.NoError(t, err)
	newEngine := managerEngine(t, memory, "new-output")
	require.NoError(t, manager.Swap(newEngine))
	removed, err := manager.RetainProgramsContext(context.Background(), map[string]struct{}{newEngine.ProgramID(): {}})
	require.NoError(t, err)
	require.Equal(t, []string{oldEngine.ProgramID()}, removed)
	state, err = memory.ReadSnapshot(context.Background(), []string{trackerKey})
	require.NoError(t, err)
	require.Equal(t, store.Missing, state[trackerKey].State)
}

type blockingCleanupStore struct {
	*store.MemoryStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingCleanupStore) DeleteInternal(ctx context.Context, keys []string) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.MemoryStore.DeleteInternal(ctx, keys)
}

func TestEngineManagerCleansTemporalStateOutsideManagerLock(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	backend := &blockingCleanupStore{MemoryStore: memory, entered: make(chan struct{}), release: make(chan struct{})}
	_, artifact := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	oldEngine, err := NewEngineFromBytes(artifact, backend, 0)
	require.NoError(t, err)
	manager, err := NewEngineManager(oldEngine)
	require.NoError(t, err)
	active := managerEngine(t, backend, "new-output")
	require.NoError(t, manager.Swap(active))

	done := make(chan error, 1)
	go func() {
		_, cleanupErr := manager.RetainProgramsContext(context.Background(), map[string]struct{}{active.ProgramID(): {}})
		done <- cleanupErr
	}()
	<-backend.entered
	activeRead := make(chan string, 1)
	go func() { activeRead <- manager.ActiveProgramID() }()
	select {
	case programID := <-activeRead:
		require.Equal(t, active.ProgramID(), programID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("manager read blocked on temporal state cleanup")
	}
	registerDone := make(chan error, 1)
	go func() { registerDone <- manager.Register(oldEngine) }()
	select {
	case <-registerDone:
		t.Fatal("program was re-registered while its temporal state was being deleted")
	case <-time.After(25 * time.Millisecond):
	}
	secondRead := make(chan string, 1)
	go func() { secondRead <- manager.ActiveProgramID() }()
	select {
	case programID := <-secondRead:
		require.Equal(t, active.ProgramID(), programID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("queued registration blocked event-facing manager reads")
	}
	close(backend.release)
	require.NoError(t, <-done)
	require.NoError(t, <-registerDone)
}

type failingCleanupStore struct {
	*store.MemoryStore
	calls int
}

func (s *failingCleanupStore) DeleteInternal(ctx context.Context, keys []string) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("cleanup unavailable")
	}
	return s.MemoryStore.DeleteInternal(ctx, keys)
}

func TestEngineManagerContinuesRetirementAfterCleanupFailure(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	backend := &failingCleanupStore{MemoryStore: memory}
	manager, err := NewEngineManager(managerEngine(t, backend, "active-output"))
	require.NoError(t, err)
	var expected []string
	for _, duration := range []string{"1m", "2m"} {
		_, artifact := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"`+duration+`"}]}`)
		engine, loadErr := NewEngineFromBytes(artifact, backend, 0)
		require.NoError(t, loadErr)
		require.NoError(t, manager.Register(engine))
		expected = append(expected, engine.ProgramID())
	}

	removed, cleanupErr := manager.RetainProgramsContext(context.Background(), map[string]struct{}{manager.ActiveProgramID(): {}})
	require.ErrorContains(t, cleanupErr, "cleanup unavailable")
	require.ElementsMatch(t, expected, removed)
	require.Equal(t, 2, backend.calls)
	removed, cleanupErr = manager.RetainProgramsContext(context.Background(), map[string]struct{}{manager.ActiveProgramID(): {}})
	require.NoError(t, cleanupErr)
	require.Empty(t, removed)
	require.Equal(t, 3, backend.calls, "failed cleanup should be retried on the next retention pass")
}

type noCleanupStore struct {
	store.ContextStore
	store.SnapshotReader
	store.Committer
}

func TestEngineManagerDropsPermanentlyUnsupportedCleanup(t *testing.T) {
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	backend := &noCleanupStore{ContextStore: memory, SnapshotReader: memory, Committer: memory}
	_, artifact := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	oldEngine, err := NewEngineFromBytes(artifact, backend, 0)
	require.NoError(t, err)
	manager, err := NewEngineManager(oldEngine)
	require.NoError(t, err)
	active := managerEngine(t, backend, "new-output")
	require.NoError(t, manager.Swap(active))

	removed, cleanupErr := manager.RetainProgramsContext(context.Background(), map[string]struct{}{active.ProgramID(): {}})
	require.Equal(t, []string{oldEngine.ProgramID()}, removed)
	require.ErrorIs(t, cleanupErr, errTemporalStateCleanupUnsupported)
	require.Empty(t, manager.pendingCleanup, "permanently unsupported cleanup must not retain engines for retry")
}
