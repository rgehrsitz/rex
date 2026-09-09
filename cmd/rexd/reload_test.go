package main

import (
	"context"
	"os"
	"path/filepath"
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
