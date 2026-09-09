package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
	"rgehrsitz/rex/pkg/observability"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

var errReloadDeferred = errors.New("ruleset reload deferred while durable events are pending")

type durableStatsReader interface {
	Stats(context.Context) (store.DurableStats, error)
}

type rulesetReloader struct {
	config       *Config
	store        store.ContextStore
	manager      *runtime.EngineManager
	metrics      *observability.Metrics
	stats        durableStatsReader
	mu           sync.Mutex
	lastAttempt  [sha256.Size]byte
	hasAttempt   bool
	lastDeferred store.DurableStats
}

func startRulesetReload(ctx context.Context, deps *RexDependencies, config *Config, metrics *observability.Metrics, stats durableStatsReader) error {
	if config.ReloadInterval == 0 {
		return nil
	}
	reloader, err := newRulesetReloader(config, deps, metrics, stats)
	if err != nil {
		return fmt.Errorf("initialize ruleset reload: %w", err)
	}
	go reloader.run(ctx)
	logging.Logger.Info().Dur("interval", config.ReloadInterval).Str("history_dir", config.ReloadHistoryDir).Str("program_id", deps.Manager.ActiveProgramID()).Msg("Ruleset reload enabled")
	return nil
}

func newRulesetReloader(config *Config, deps *RexDependencies, metrics *observability.Metrics, stats durableStatsReader) (*rulesetReloader, error) {
	r := &rulesetReloader{config: config, store: deps.Store, manager: deps.Manager, metrics: metrics, stats: stats}
	if err := os.MkdirAll(config.ReloadHistoryDir, 0o750); err != nil {
		return nil, fmt.Errorf("create ruleset history directory: %w", err)
	}
	if err := cleanupReloadTemps(config.ReloadHistoryDir); err != nil {
		return nil, err
	}
	if err := r.loadHistory(); err != nil {
		return nil, err
	}
	data, err := readRulesetArtifact(config.BytecodeFile)
	if err != nil {
		return nil, fmt.Errorf("read active ruleset for history: %w", err)
	}
	if err := r.archive(data, deps.Manager.ActiveProgramID()); err != nil {
		return nil, err
	}
	r.lastAttempt = sha256.Sum256(data)
	r.hasAttempt = true
	return r, nil
}

func (r *rulesetReloader) run(ctx context.Context) {
	ticker := time.NewTicker(r.config.ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			programID, err := r.reload(ctx)
			if errors.Is(err, errReloadDeferred) {
				r.metrics.RecordRulesetReloadDeferred()
				stats := r.deferredStats()
				logging.Logger.Info().Str("active_program_id", r.manager.ActiveProgramID()).Int64("pending", stats.Pending).Int64("lag", stats.Lag).Msg("Ruleset reload deferred while durable events are pending")
				continue
			}
			if err != nil {
				r.metrics.RecordRulesetReloadFailure()
				logging.Logger.Error().Err(err).Str("active_program_id", r.manager.ActiveProgramID()).Msg("Ruleset reload rejected; retaining active program")
				continue
			}
			if programID != "" {
				r.metrics.RecordRulesetReloadSuccess()
				logging.Logger.Info().Str("program_id", programID).Msg("Ruleset reload completed")
			}
		}
	}
}

// reload returns an empty program ID when the configured artifact has not
// changed since the last validation attempt.
func (r *rulesetReloader) reload(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := readRulesetArtifact(r.config.BytecodeFile)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	if r.hasAttempt && digest == r.lastAttempt {
		return "", nil
	}
	r.lastAttempt, r.hasAttempt = digest, true
	candidate, err := runtime.NewEngineFromBytes(data, r.store, r.config.PriorityThreshold)
	if err != nil {
		return "", fmt.Errorf("validate reload candidate: %w", err)
	}
	if !compiler.IsBatchVersion(candidate.BytecodeVersion()) {
		return "", fmt.Errorf("ruleset reload requires a batch artifact")
	}
	if err := configureEngine(candidate, r.config); err != nil {
		return "", fmt.Errorf("configure reload candidate: %w", err)
	}
	candidate.SetExecutionObserver(r.metrics)
	if candidate.ProgramID() == r.manager.ActiveProgramID() {
		if err := r.archive(data, candidate.ProgramID()); err != nil {
			r.hasAttempt = false
			return "", err
		}
		return "", nil
	}
	if r.stats != nil {
		stats, err := r.stats.Stats(ctx)
		if err != nil {
			r.hasAttempt = false
			return "", fmt.Errorf("check durable backlog before reload: %w", err)
		}
		if stats.Pending > 0 {
			// Permit another attempt without requiring the artifact file to change.
			r.lastDeferred = stats
			r.hasAttempt = false
			return "", errReloadDeferred
		}
	}
	if err := r.archive(data, candidate.ProgramID()); err != nil {
		r.hasAttempt = false
		return "", err
	}
	if err := r.manager.Swap(candidate); err != nil {
		r.hasAttempt = false
		return "", err
	}
	keep, err := r.historyProgramIDs()
	if err != nil {
		// Activation is already complete. Retaining extra validated history is
		// safer than removing an engine when the directory cannot be inspected.
		logging.Logger.Error().Err(err).Msg("Ruleset activated but in-memory history could not be pruned")
	} else {
		removed, cleanupErr := r.manager.RetainProgramsContext(ctx, keep)
		if len(removed) > 0 {
			logging.Logger.Info().Strs("program_ids", removed).Msg("Released ruleset programs removed from artifact history")
		}
		if cleanupErr != nil {
			logging.Logger.Error().Err(cleanupErr).Msg("Ruleset activated but retired temporal state could not be cleaned")
		}
	}
	return candidate.ProgramID(), nil
}

func (r *rulesetReloader) deferredStats() store.DurableStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastDeferred
}

func (r *rulesetReloader) historyProgramIDs() (map[string]struct{}, error) {
	paths, err := filepath.Glob(filepath.Join(r.config.ReloadHistoryDir, "*.bytecode"))
	if err != nil {
		return nil, fmt.Errorf("list ruleset history: %w", err)
	}
	programs := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		programID := strings.TrimSuffix(filepath.Base(path), ".bytecode")
		programs[programID] = struct{}{}
	}
	return programs, nil
}

func (r *rulesetReloader) loadHistory() error {
	paths, err := filepath.Glob(filepath.Join(r.config.ReloadHistoryDir, "*.bytecode"))
	if err != nil {
		return fmt.Errorf("list ruleset history: %w", err)
	}
	sort.Strings(paths)
	if len(paths) > r.config.ReloadHistoryMaxFiles {
		return fmt.Errorf("ruleset history contains %d artifacts; maximum is %d", len(paths), r.config.ReloadHistoryMaxFiles)
	}
	for _, path := range paths {
		data, err := readRulesetArtifact(path)
		if err != nil {
			return fmt.Errorf("read historical ruleset %s: %w", path, err)
		}
		engine, err := runtime.NewEngineFromBytes(data, r.store, r.config.PriorityThreshold)
		if err != nil || !compiler.IsBatchVersion(engine.BytecodeVersion()) {
			return fmt.Errorf("load historical ruleset %s: valid batch artifact required", path)
		}
		if filepath.Base(path) != engine.ProgramID()+".bytecode" {
			return fmt.Errorf("historical ruleset %s does not match its program digest", path)
		}
		if err := configureEngine(engine, r.config); err != nil {
			return fmt.Errorf("configure historical ruleset %s: %w", path, err)
		}
		engine.SetExecutionObserver(r.metrics)
		if engine.ProgramID() == r.manager.ActiveProgramID() {
			continue
		}
		if err := r.manager.Register(engine); err != nil {
			return err
		}
	}
	return nil
}

func (r *rulesetReloader) archive(data []byte, programID string) error {
	if programID == "" || programID != artifactDigest(data) {
		return fmt.Errorf("refuse to archive ruleset with an invalid program digest")
	}
	path := filepath.Join(r.config.ReloadHistoryDir, programID+".bytecode")
	if _, err := os.Stat(path); err == nil {
		existing, readErr := readRulesetArtifact(path)
		if readErr != nil || artifactDigest(existing) != programID {
			return fmt.Errorf("existing ruleset history artifact %s does not match program digest", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect ruleset history: %w", err)
	}
	paths, err := filepath.Glob(filepath.Join(r.config.ReloadHistoryDir, "*.bytecode"))
	if err != nil {
		return fmt.Errorf("list ruleset history: %w", err)
	}
	if len(paths) >= r.config.ReloadHistoryMaxFiles {
		return fmt.Errorf("ruleset history is full (%d artifacts); preserve or remove artifacts explicitly", r.config.ReloadHistoryMaxFiles)
	}
	tmp, err := os.CreateTemp(r.config.ReloadHistoryDir, ".ruleset-*.tmp")
	if err != nil {
		return fmt.Errorf("create ruleset history file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	err = tmp.Chmod(0o640)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write ruleset history: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish ruleset history: %w", err)
	}
	return nil
}

func cleanupReloadTemps(historyDir string) error {
	paths, err := filepath.Glob(filepath.Join(historyDir, ".ruleset-*.tmp"))
	if err != nil {
		return fmt.Errorf("list stale ruleset history files: %w", err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale ruleset history file %s: %w", path, err)
		}
	}
	return nil
}

func readRulesetArtifact(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, compiler.MaxProgramBytes+17))
	if err != nil {
		return nil, err
	}
	if len(data) > compiler.MaxProgramBytes+16 {
		return nil, fmt.Errorf("artifact exceeds byte limit")
	}
	return data, nil
}

func artifactDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return strings.ToLower(hex.EncodeToString(digest[:]))
}
