package runtime

import (
	"context"
	"fmt"
	"sync"

	"rgehrsitz/rex/pkg/store"
)

// EngineManager pins each event to one Engine while allowing validated
// programs to be installed atomically between events.
type EngineManager struct {
	mu       sync.RWMutex
	active   *Engine
	programs map[string]*Engine
	observer ExecutionObserver
}

func NewEngineManager(active *Engine) (*EngineManager, error) {
	if active == nil {
		return nil, fmt.Errorf("active engine is required")
	}
	m := &EngineManager{active: active, programs: make(map[string]*Engine)}
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
	if engine == nil || engine.ProgramID() == "" || engine.BytecodeVersion() != 4 {
		return fmt.Errorf("historical engine must contain a v4 program ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.programs[engine.ProgramID()]; exists {
		return nil
	}
	engine.SetExecutionObserver(m.observer)
	m.programs[engine.ProgramID()] = engine
	return nil
}

// Swap waits for the current event boundary and then publishes candidate for
// all subsequently received events.
func (m *EngineManager) Swap(candidate *Engine) error {
	if candidate == nil || candidate.ProgramID() == "" || candidate.BytecodeVersion() != 4 {
		return fmt.Errorf("reload candidate must be a validated v4 engine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate.SetExecutionObserver(m.observer)
	m.programs[candidate.ProgramID()] = candidate
	m.active = candidate
	return nil
}

// RetainPrograms releases inactive historical engines that the operator has
// removed from durable artifact history. The active engine is always retained.
func (m *EngineManager) RetainPrograms(keep map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for programID, engine := range m.programs {
		if engine == m.active {
			continue
		}
		if _, ok := keep[programID]; ok {
			continue
		}
		engine.Shutdown()
		delete(m.programs, programID)
	}
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
