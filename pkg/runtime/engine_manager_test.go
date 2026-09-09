package runtime

import (
	"context"
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
	manager.RetainPrograms(map[string]struct{}{newEngine.ProgramID(): {}})
	queue := &durableQueueProbe{
		event:  store.DurableEvent{ID: "2-0", Payload: `{"a":1}`, Recovered: true},
		pinned: oldEngine.ProgramID(), maxAttempts: 3,
	}
	_, err = manager.ProcessNextDurable(context.Background(), queue)
	require.ErrorIs(t, err, store.ErrDurableProgramMismatch)
}
