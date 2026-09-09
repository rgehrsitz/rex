package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

// EngineManager pins each event to one Engine while allowing validated
// programs to be installed atomically between events.
type EngineManager struct {
	mu             sync.RWMutex
	cleanupMu      sync.Mutex
	active         *Engine
	programs       map[string]*Engine
	pendingCleanup map[string]*Engine
	observer       ExecutionObserver
}

func NewEngineManager(active *Engine) (*EngineManager, error) {
	if active == nil {
		return nil, fmt.Errorf("active engine is required")
	}
	m := &EngineManager{active: active, programs: make(map[string]*Engine), pendingCleanup: make(map[string]*Engine)}
	if active.ProgramID() != "" {
		m.programs[active.ProgramID()] = active
	}
	return m, nil
}

func (m *EngineManager) ActiveProgramID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.ProgramID()
}

func (m *EngineManager) BytecodeVersion() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.BytecodeVersion()
}

// Register makes an archived program available to pinned durable retries
// without changing the program used for new events.
func (m *EngineManager) Register(engine *Engine) error {
	if engine == nil || engine.ProgramID() == "" || !compiler.IsBatchVersion(engine.BytecodeVersion()) {
		return fmt.Errorf("historical engine must contain a batch program ID")
	}
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.programs[engine.ProgramID()]; exists {
		return nil
	}
	delete(m.pendingCleanup, engine.ProgramID())
	engine.SetExecutionObserver(m.observer)
	m.programs[engine.ProgramID()] = engine
	return nil
}

// Swap waits for the current event boundary and then publishes candidate for
// all subsequently received events.
func (m *EngineManager) Swap(candidate *Engine) error {
	if candidate == nil || candidate.ProgramID() == "" || !compiler.IsBatchVersion(candidate.BytecodeVersion()) {
		return fmt.Errorf("reload candidate must be a validated batch engine")
	}
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pendingCleanup, candidate.ProgramID())
	candidate.SetExecutionObserver(m.observer)
	m.programs[candidate.ProgramID()] = candidate
	m.active = candidate
	return nil
}

// RetainPrograms releases inactive historical engines that the operator has
// removed from durable artifact history. The active engine is always retained.
// Deprecated: use RetainProgramsContext so cleanup failures are observable.
func (m *EngineManager) RetainPrograms(keep map[string]struct{}) []string {
	removed, _ := m.RetainProgramsContext(context.Background(), keep)
	return removed
}

// RetainProgramsContext also removes private temporal state after an inactive
// artifact leaves the retained rollback and recovery set.
func (m *EngineManager) RetainProgramsContext(ctx context.Context, keep map[string]struct{}) ([]string, error) {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()

	m.mu.Lock()
	type retiredProgram struct {
		programID string
		engine    *Engine
	}
	var retired []retiredProgram
	for programID, engine := range m.programs {
		if engine == m.active {
			continue
		}
		if _, ok := keep[programID]; ok {
			continue
		}
		delete(m.programs, programID)
		m.pendingCleanup[programID] = engine
		retired = append(retired, retiredProgram{programID: programID, engine: engine})
	}
	m.mu.Unlock()
	sort.Slice(retired, func(i, j int) bool { return retired[i].programID < retired[j].programID })
	removed := make([]string, 0, len(retired))
	for _, program := range retired {
		program.engine.Shutdown()
		removed = append(removed, program.programID)
	}

	m.mu.RLock()
	pending := make([]retiredProgram, 0, len(m.pendingCleanup))
	for programID, engine := range m.pendingCleanup {
		pending = append(pending, retiredProgram{programID: programID, engine: engine})
	}
	m.mu.RUnlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].programID < pending[j].programID })
	var cleanupErr error
	for _, program := range pending {
		err := program.engine.deleteTemporalState(ctx)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("clean temporal state for program %s (keys %v): %w", program.programID, program.engine.temporalStateKeys(), err))
		}
		if err == nil || errors.Is(err, errTemporalStateCleanupUnsupported) {
			m.mu.Lock()
			if m.pendingCleanup[program.programID] == program.engine {
				delete(m.pendingCleanup, program.programID)
			}
			m.mu.Unlock()
		}
	}
	return removed, cleanupErr
}

func (m *EngineManager) SetExecutionObserver(observer ExecutionObserver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observer = observer
	m.active.SetExecutionObserver(observer)
	for _, engine := range m.programs {
		if engine != m.active {
			engine.SetExecutionObserver(observer)
		}
	}
}

func (m *EngineManager) ProcessFactUpdateContext(ctx context.Context, factName string, factValue interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.ProcessFactUpdateContext(ctx, factName, factValue)
}

func (m *EngineManager) ProcessBatchContext(ctx context.Context, event map[string]interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.ProcessBatchContext(ctx, event)
}

func (m *EngineManager) ProcessNextDurable(ctx context.Context, queue VersionedDurableQueue) (DurableProcessResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	event, receiveErr := queue.Next(ctx)
	if event.ID == "" {
		return DurableProcessResult{}, receiveErr
	}
	programID, err := queue.PinnedProgram(ctx, event.ID)
	if err != nil {
		return DurableProcessResult{EventID: event.ID, Recovered: event.Recovered}, err
	}
	engine := m.active
	if programID != "" {
		engine = m.programs[programID]
		if engine == nil {
			return DurableProcessResult{EventID: event.ID, Recovered: event.Recovered}, fmt.Errorf("%w: event %s requires unavailable program %s", store.ErrDurableProgramMismatch, event.ID, programID)
		}
	}
	return engine.processDurableEvent(ctx, queue, event, receiveErr)
}

func (m *EngineManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active.Shutdown()
	for _, engine := range m.programs {
		if engine != m.active {
			engine.Shutdown()
		}
	}
}
