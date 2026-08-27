package runtime

import "rgehrsitz/rex/pkg/compiler"

// ExecutionObserver receives aggregate runtime outcomes. Implementations must
// be safe for concurrent use because an Engine may process several events.
type ExecutionObserver interface {
	RuleFired(ruleName string)
	ActionSucceeded(actionType string)
	ActionSkipped(actionType string)
	ActionFailed(actionType string, err error)
}

// SetExecutionObserver configures optional runtime outcome observation. Set it
// during engine initialization, before events are processed.
func (e *Engine) SetExecutionObserver(observer ExecutionObserver) {
	e.executionObserver = observer
}

func (e *Engine) recordRuleFired(ruleName string) {
	if e.executionObserver != nil {
		e.executionObserver.RuleFired(ruleName)
	}
}

func (e *Engine) recordActionSucceeded(action compiler.Action) {
	if e.executionObserver != nil {
		e.executionObserver.ActionSucceeded(action.Type)
	}
}

func (e *Engine) recordActionSkipped(action compiler.Action) {
	if e.executionObserver != nil {
		e.executionObserver.ActionSkipped(action.Type)
	}
}

func (e *Engine) recordActionFailed(action compiler.Action, err error) {
	if e.executionObserver != nil {
		e.executionObserver.ActionFailed(action.Type, err)
	}
}
