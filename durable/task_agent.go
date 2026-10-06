package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// TaskAgent is a detached phase-local agent selection. Host registrations and
// wrappers remain process-local; stored instructions/settings are copied.
// Every Agent call returns an independent copy, unlike upstream object identity.
type TaskAgent struct {
	Configuration AgentChange
	Extensions    []string
	Tools         []goai.Tool
	Sections      []PromptSection
}
type phaseAgentResolution struct {
	done  chan struct{}
	agent TaskAgent
	err   error
	hooks []TaskHookRegistration
}

// Agent resolves once on first use with the phase's registry snapshot and the
// current committed conversation agent. Cancelling one caller does not cancel
// the shared resolution. Failures remain cached until the next phase.
// phaseSelection supplies builtin adapters the same captured registration
// selection used by lazy agent/custom-hook resolution. Never retarget an
// in-flight provider/tool/compaction phase to newly installed extensions.
func (r *TaskRuntime) phaseSelection() *Registry {
	r.phaseMu.RLock()
	defer r.phaseMu.RUnlock()
	if r.registry.selection != nil {
		return r.registry.selection
	}
	return NewRegistry()
}

func (r *TaskRuntime) Agent(ctx context.Context) (TaskAgent, error) {
	resolution, err := r.phaseAgent(ctx)
	if err != nil {
		return TaskAgent{}, err
	}
	return copyTaskAgent(resolution.agent, r.harness.session.limits)
}

// Return the captured resolution itself to internal hook dispatch, so a later
// phase cache replacement cannot retarget a caller that already awaited it.
func (r *TaskRuntime) phaseAgent(ctx context.Context) (*phaseAgentResolution, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, reject("nil context")
	}
	for {
		r.phaseMu.Lock()
		if err := r.check(); err != nil {
			r.phaseMu.Unlock()
			return nil, err
		}
		if r.phaseDraining {
			ready := r.phaseReady
			r.phaseMu.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-r.context.Done():
				if err := r.check(); err != nil {
					return nil, err
				}
				return nil, r.context.Err()
			}
		}
		resolution := r.agentResolution
		if resolution == nil {
			resolution = &phaseAgentResolution{done: make(chan struct{})}
			r.agentResolution = resolution
			snapshot := r.registry
			go r.resolvePhaseAgent(resolution, snapshot)
		}
		r.phaseMu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.context.Done():
			if err := r.check(); err != nil {
				return nil, err
			}
			return nil, r.context.Err()
		case <-resolution.done:
		}
		if err := r.check(); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if resolution.err != nil {
			return nil, resolution.err
		}
		return resolution, nil
	}
}

// Agent resolution is joined at a phase boundary and actual host return. Pure
// wrappers that ignore cancellation therefore remain owned host work.
func (r *TaskRuntime) joinPhaseAgent() {
	r.phaseMu.Lock()
	if !r.phaseDraining {
		r.phaseDraining = true
		r.phaseReady = make(chan struct{})
	}
	resolution := r.agentResolution
	r.phaseMu.Unlock()
	if resolution != nil {
		<-resolution.done
	}
}

// Caller holds phaseMu. The previous generation was drained before Session
// admission, so this operation never overwrites unjoined resolver work.
func (r *TaskRuntime) publishAgentPhaseLocked(snapshot TaskRegistrySnapshot) {
	r.registry = snapshot
	r.agentResolution = nil
	r.phaseDraining = false
	if r.phaseReady != nil {
		close(r.phaseReady)
		r.phaseReady = nil
	}
}

func (r *TaskRuntime) resolvePhaseAgent(resolution *phaseAgentResolution, snapshot TaskRegistrySnapshot) {
	defer close(resolution.done)
	defer func() {
		if recover() != nil {
			resolution.err = reject("agent resolution panic")
		}
	}()
	var agent agentState
	resolution.err = r.harness.session.readTasks(r.context, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		if _, exists := state.Conversations[r.conversation]; !exists {
			return reject("unknown conversation")
		}
		if doc, ok := agentDocument(state, r.conversation); ok {
			return fromObject(doc.Value, &agent, r.harness.session.limits)
		}
		return nil
	})
	if resolution.err != nil {
		return
	}
	resolution.agent, resolution.hooks, resolution.err = r.harness.resolveTaskAgent(agent, snapshot)
}

func (h *Harness) resolveTaskAgent(agent agentState, snapshot TaskRegistrySnapshot) (TaskAgent, []TaskHookRegistration, error) {
	agent.Settings = h.resolvedSettings(agent.Settings)
	selected := h.selectionAgent(agent)
	registry := snapshot.selection
	if registry == nil {
		registry = NewRegistry()
	}
	offers, _, sections, _, err := registry.selectedGeneration(selected, h.session.limits, h.scheduler.report)
	if err != nil {
		return TaskAgent{}, nil, err
	}
	sections = withInstructionSection(sections, agent.Instructions)
	tools, err := protocolTools(offers, h.session.limits)
	if err != nil {
		return TaskAgent{}, nil, err
	}
	extensions := []string{}
	hooks := []TaskHookRegistration{}
	registry.mu.RLock()
	for _, extension := range registry.selectedExtensionsLocked(selected) {
		extensions = append(extensions, extension.name)
		hooks = append(hooks, extension.taskHooks...)
	}
	registry.mu.RUnlock()
	if agent.ThinkingLevel == "" {
		agent.ThinkingLevel = "off"
	}
	value := TaskAgent{Configuration: AgentChange{Model: agent.Model, ThinkingLevel: agent.ThinkingLevel, Name: agent.Name, Cwd: agent.Cwd, SystemPrompt: agent.SystemPrompt, Instructions: agent.Instructions, Settings: agent.Settings, Extensions: agent.Extensions, Tools: agent.Tools, ExtensionFilter: agent.ExtensionFilter, ToolsRemoved: agent.ToolsRemoved}, Extensions: extensions, Tools: tools, Sections: sections}
	return value, hooks, nil
}
func copyTaskAgent(agent TaskAgent, limits Limits) (TaskAgent, error) {
	value, err := dtoObject(agent.Configuration, limits)
	if err != nil {
		return TaskAgent{}, err
	}
	var config AgentChange
	if err := fromObject(value, &config, limits); err != nil {
		return TaskAgent{}, err
	}
	result := TaskAgent{Configuration: config, Extensions: append([]string{}, agent.Extensions...), Tools: make([]goai.Tool, len(agent.Tools)), Sections: append([]PromptSection{}, agent.Sections...)}
	for i, tool := range agent.Tools {
		result.Tools[i] = tool
		result.Tools[i].Parameters = append([]byte(nil), tool.Parameters...)
	}
	return result, nil
}
