// Package tooling provides deterministic, offline v4 authoring tools. It never
// creates network clients; all state and derived writes belong to one replay.
package tooling

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

const SchemaVersion = 1
const MaxDocumentBytes = 32 << 20
const MaxEvents = 1000

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxDocumentBytes {
		return nil, fmt.Errorf("document exceeds %d bytes", MaxDocumentBytes)
	}
	return b, nil
}
func Decode(data []byte, value interface{}) error {
	if len(data) > MaxDocumentBytes {
		return fmt.Errorf("document exceeds byte limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON document")
	}
	return nil
}

type Input struct {
	ID    string                 `json:"id"`
	Facts map[string]interface{} `json:"facts"`
}
type Expectation struct {
	FinalState    map[string]interface{}   `json:"final_state"`
	Actions       []runtime.ActionProposal `json:"actions"`
	ErrorContains string                   `json:"error_contains,omitempty"`
}
type Scenario struct {
	SchemaVersion int                    `json:"schema_version"`
	Name          string                 `json:"name"`
	InitialState  map[string]interface{} `json:"initial_state"`
	Events        []Input                `json:"events"`
	Limits        *runtime.Limits        `json:"limits,omitempty"`
	Expect        *Expectation           `json:"expect,omitempty"`
}
type Manifest struct {
	SchemaVersion     int    `json:"schema_version"`
	ExecutionContract uint32 `json:"execution_contract"`
	SourceSHA256      string `json:"source_sha256"`
	ArtifactSHA256    string `json:"artifact_sha256"`
	ArtifactBytes     int    `json:"artifact_bytes"`
}

func NewManifest(source, artifact []byte) Manifest {
	return Manifest{SchemaVersion, compiler.BatchVersion, Digest(source), Digest(artifact), len(artifact)}
}

type Bundle struct {
	SchemaVersion int      `json:"schema_version"`
	Manifest      Manifest `json:"manifest"`
	// A string preserves exact authored bytes across outer JSON reformatting.
	Source   string   `json:"source"`
	Scenario Scenario `json:"scenario"`
}

func NewBundle(source []byte, scenario Scenario) (Bundle, error) {
	artifact, err := compiler.CompileBatch(source)
	if err != nil {
		return Bundle{}, err
	}
	if scenario.Limits == nil {
		l := runtime.DefaultLimits()
		scenario.Limits = &l
	}
	b := Bundle{SchemaVersion, NewManifest(source, artifact), string(source), scenario}
	_, err = b.Validate()
	return b, err
}
func (b Bundle) Validate() ([]byte, error) {
	if b.SchemaVersion != SchemaVersion || b.Manifest.SchemaVersion != SchemaVersion || b.Manifest.ExecutionContract != compiler.BatchVersion {
		return nil, fmt.Errorf("unsupported replay schema or execution contract; tooling requires schema 1 / v4")
	}
	if b.Scenario.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported scenario schema")
	}
	if b.Scenario.Name == "" || len(b.Scenario.Name) > 255 || b.Scenario.InitialState == nil || b.Scenario.Events == nil || b.Scenario.Limits == nil {
		return nil, fmt.Errorf("replay requires name, complete initial_state, ordered events and explicit limits")
	}
	if err := b.Scenario.Limits.Validate(); err != nil {
		return nil, err
	}
	if len(b.Scenario.Events) > MaxEvents {
		return nil, fmt.Errorf("replay exceeds %d events", MaxEvents)
	}
	if len(b.Scenario.InitialState) > 65536 {
		return nil, fmt.Errorf("initial state exceeds fact limit")
	}
	initial, err := json.Marshal(b.Scenario.InitialState)
	if err != nil {
		return nil, err
	}
	if len(initial) > store.MaxSnapshotBytes {
		return nil, fmt.Errorf("initial state exceeds byte limit")
	}
	seen := map[string]bool{}
	for _, event := range b.Scenario.Events {
		if event.ID == "" || len(event.ID) > 255 || seen[event.ID] || event.Facts == nil {
			return nil, fmt.Errorf("events require unique nonempty ids and facts")
		}
		seen[event.ID] = true
		if err := runtime.ValidateEvent(event.Facts, *b.Scenario.Limits); err != nil {
			return nil, fmt.Errorf("event %q: %w", event.ID, err)
		}
		for key, value := range event.Facts {
			if key == "" || len(key) > 255 || !store.Scalar(value) {
				return nil, fmt.Errorf("event %q contains invalid scalar fact %q", event.ID, key)
			}
		}
	}
	artifact, err := compiler.CompileBatch([]byte(b.Source))
	if err != nil {
		return nil, err
	}
	if b.Manifest != NewManifest([]byte(b.Source), artifact) {
		return nil, fmt.Errorf("replay source/artifact digest or provenance mismatch")
	}
	return artifact, nil
}

type EventResult struct {
	ID    string              `json:"id"`
	Chain runtime.ChainResult `json:"chain"`
	Error string              `json:"error,omitempty"`
}
type Report struct {
	SchemaVersion int                    `json:"schema_version"`
	Manifest      Manifest               `json:"manifest"`
	Name          string                 `json:"name"`
	Mode          string                 `json:"mode"`
	Events        []EventResult          `json:"events"`
	FinalState    map[string]interface{} `json:"final_state"`
	Error         string                 `json:"error,omitempty"`
}

// Replay uses the production coordinator and evaluator with a private memory
// adapter. No caller can inject a transport or external committer here.
func Replay(ctx context.Context, b Bundle) (Report, error) {
	artifact, err := b.Validate()
	if err != nil {
		return Report{}, err
	}
	p, err := runtime.LoadProgram(artifact)
	if err != nil {
		return Report{}, err
	}
	memory, err := store.NewMemoryStore(b.Scenario.InitialState)
	if err != nil {
		return Report{}, err
	}
	defer memory.Close()
	coordinator, err := runtime.NewCoordinator(p, memory, memory, *b.Scenario.Limits)
	if err != nil {
		return Report{}, err
	}
	r := Report{SchemaVersion: SchemaVersion, Manifest: b.Manifest, Name: b.Scenario.Name, Mode: "offline", Events: []EventResult{}}
	reportBytes := 0
	for _, event := range b.Scenario.Events {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		// Validate before persisting input without consuming evaluation work.
		if err := runtime.ValidateEvent(event.Facts, *b.Scenario.Limits); err != nil {
			return r, fmt.Errorf("event %q: %w", event.ID, err)
		}
		for key, value := range event.Facts {
			if err := memory.SetFactContext(ctx, key, value); err != nil {
				return r, err
			}
		}
		chain, evalErr := coordinator.Process(ctx, event.ID, event.Facts)
		entry := EventResult{ID: event.ID, Chain: chain}
		if evalErr != nil {
			entry.Error = evalErr.Error()
			r.Error = entry.Error
		}
		r.Events = append(r.Events, entry)
		entryJSON, err := JSON(entry)
		if err != nil {
			return r, err
		}
		reportBytes += len(entryJSON)
		if reportBytes > MaxDocumentBytes {
			return r, fmt.Errorf("replay report exceeds byte limit")
		}
		// Publications are not replay inputs; the coordinator already ran derived rounds.
		if _, err := memory.DrainPublications(); err != nil {
			return r, err
		}
		state, err := memory.Snapshot()
		if err != nil {
			return r, err
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return r, err
		}
		if len(state) > 65536 || len(encoded) > store.MaxSnapshotBytes {
			return r, fmt.Errorf("replay retained state exceeds limit")
		}
		if evalErr != nil {
			break
		}
	}
	r.FinalState, err = memory.Snapshot()
	return r, err
}
func Actions(r Report) []runtime.ActionProposal {
	out := []runtime.ActionProposal{}
	for _, e := range r.Events {
		for _, round := range e.Chain.Rounds {
			out = append(out, round.Evaluation.Actions...)
		}
	}
	return out
}

type Comparison struct {
	SchemaVersion int      `json:"schema_version"`
	Changed       bool     `json:"changed"`
	Changes       []string `json:"changes"`
	Before        Report   `json:"before"`
	After         Report   `json:"after"`
}

func Compare(ctx context.Context, b Bundle, source []byte) (Comparison, error) {
	before, err := Replay(ctx, b)
	if err != nil {
		return Comparison{}, err
	}
	candidate, err := NewBundle(source, b.Scenario)
	if err != nil {
		return Comparison{}, err
	}
	after, err := Replay(ctx, candidate)
	if err != nil {
		return Comparison{}, err
	}
	changes := []string{}
	if !reflect.DeepEqual(Actions(before), Actions(after)) {
		changes = append(changes, "actions")
	}
	if !reflect.DeepEqual(before.FinalState, after.FinalState) {
		changes = append(changes, "final_state")
	}
	if !reflect.DeepEqual(before.Events, after.Events) {
		changes = append(changes, "evaluation_trace")
	}
	return Comparison{SchemaVersion, len(changes) > 0, changes, before, after}, nil
}
