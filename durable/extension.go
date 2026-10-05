package durable

// Extension publishes a named group of tools, tasks and prompt sections as one
// immutable registry revision. Replacing a name retains its installation order.
type Extension struct {
	Name            string
	Tools           []ToolRegistration
	Tasks           []*TaskDefinition
	Sections        []PromptSection
	Hooks           GenerationHooks
	ToolHooks       ToolHooks
	CompactionHooks CompactionHooks
}
type installedExtension struct {
	source          *Extension
	name            string
	tools           map[string]registeredTool
	tasks           map[string]*TaskDefinition
	sections        []PromptSection
	hooks           GenerationHooks
	toolHooks       ToolHooks
	compactionHooks CompactionHooks
}

func (r *Registry) effectiveToolsLocked() map[string]registeredTool {
	tools := make(map[string]registeredTool, len(r.tools))
	for name, tool := range r.tools {
		tools[name] = tool
	}
	for _, ext := range r.extensions {
		for name, tool := range ext.tools {
			tools[name] = tool
		}
	}
	return tools
}
func (r *Registry) effectiveTasksLocked() map[string]*TaskDefinition {
	tasks := make(map[string]*TaskDefinition, len(r.tasks))
	for name, task := range r.tasks {
		tasks[name] = task
	}
	for _, ext := range r.extensions {
		for name, task := range ext.tasks {
			tasks[name] = task
		}
	}
	return tasks
}
func (r *Registry) Install(extension *Extension) error {
	if r == nil || extension == nil || !validKind(extension.Name) {
		return reject("invalid extension")
	}
	prepared := installedExtension{source: extension, name: extension.Name, tools: map[string]registeredTool{}, tasks: map[string]*TaskDefinition{}, sections: append([]PromptSection(nil), extension.Sections...), hooks: extension.Hooks, toolHooks: extension.ToolHooks, compactionHooks: extension.CompactionHooks}
	trial := NewRegistry()
	for _, tool := range extension.Tools {
		if _, exists := prepared.tools[tool.Definition.Name]; exists {
			return reject("duplicate extension tool")
		}
		if err := trial.Register(tool); err != nil {
			return err
		}
		prepared.tools[tool.Definition.Name] = trial.tools[tool.Definition.Name]
	}
	for _, task := range extension.Tasks {
		if task == nil || !task.constructed {
			return reject("invalid extension task")
		}
		if _, exists := prepared.tasks[task.Kind()]; exists {
			return reject("duplicate extension task")
		}
		prepared.tasks[task.Kind()] = task
	}
	keys := map[string]bool{}
	for _, section := range prepared.sections {
		if !sectionKey.MatchString(section.Key) || section.Key == "instructions" || section.Render == nil || keys[section.Key] {
			return reject("invalid extension section")
		}
		keys[section.Key] = true
	}
	r.mu.Lock()
	index := -1
	for i, ext := range r.extensions {
		if ext.name == prepared.name {
			index = i
			continue
		}
		for name := range prepared.tasks {
			if ext.tasks[name] != nil {
				r.mu.Unlock()
				return reject("extension task collision")
			}
		}
	}
	for name := range prepared.tasks {
		if r.tasks[name] != nil {
			r.mu.Unlock()
			return reject("extension task collision")
		}
	}
	next := append([]installedExtension(nil), r.extensions...)
	if index < 0 {
		next = append(next, prepared)
	} else {
		next[index] = prepared
	}
	if len(next) > DefaultLimits().MaxPage {
		r.mu.Unlock()
		return reject("extension registry capacity")
	}
	toolNames, taskNames := map[string]bool{}, map[string]bool{}
	for name := range r.tools {
		toolNames[name] = true
	}
	for name := range r.tasks {
		taskNames[name] = true
	}
	for _, extension := range next {
		for name := range extension.tools {
			toolNames[name] = true
		}
		for name := range extension.tasks {
			taskNames[name] = true
		}
	}
	if len(toolNames) > MaxTools || len(taskNames) > DefaultLimits().MaxPage {
		r.mu.Unlock()
		return reject("extension resource capacity")
	}
	r.extensions = next
	wake := r.taskListenersLocked()
	r.mu.Unlock()
	for _, notify := range wake {
		notify()
	}
	return nil
}
func (r *Registry) Uninstall(extension *Extension) {
	if r == nil || extension == nil {
		return
	}
	r.mu.Lock()
	found := false
	for i, ext := range r.extensions {
		if ext.source == extension {
			r.extensions = append(r.extensions[:i], r.extensions[i+1:]...)
			found = true
			break
		}
	}
	wake := r.taskListenersLocked()
	r.mu.Unlock()
	if found {
		for _, notify := range wake {
			notify()
		}
	}
}
func (r *Registry) Installed() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, len(r.extensions))
	for i, ext := range r.extensions {
		names[i] = ext.name
	}
	return names
}
func (r *Registry) PromptSections() []PromptSection {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sections := []PromptSection{}
	for _, ext := range r.extensions {
		sections = append(sections, ext.sections...)
	}
	return sections
}
