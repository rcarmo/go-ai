package durable

func codemodeBuiltinDeclarations(paid bool) []CodemodeGlobalDeclaration {
	globals := []CodemodeGlobalDeclaration{
		{Name: "searchTools", Description: "Search the pinned callable tool catalogue.", Signature: "(query: string, options?: { limit?: number; namespace?: string }): Promise<Array<{ name: string; description: string }>>"},
		{Name: "describeTool", Description: "Describe one callable tool from the pinned catalogue.", Signature: "(name: string): Promise<string>"},
		{Name: "describeNamespace", Description: "Describe a pinned tool namespace.", Signature: "(name: string): Promise<{ name: string; description: string; instructions: string; tools: string[] }>"},
	}
	if paid {
		globals = append(globals,
			CodemodeGlobalDeclaration{Name: "models.getModelsOfType", Signature: "(type: 'chat' | 'image' | 'classifier', provider?: string): Promise<unknown[]>"},
			CodemodeGlobalDeclaration{Name: "models.getAvailableOfType", Signature: "(type: 'chat' | 'image' | 'classifier', provider?: string): Promise<unknown[]>"},
			CodemodeGlobalDeclaration{Name: "models.getModelOfType", Signature: "(type: 'chat' | 'image' | 'classifier', provider: string, id: string): Promise<unknown | null>"},
			CodemodeGlobalDeclaration{Name: "models.classify", Signature: "(model: { provider: string; id: string }, context: { state: { [key: string]: unknown }; questions: { [key: string]: unknown }; images?: unknown[] }): Promise<unknown>"},
			CodemodeGlobalDeclaration{Name: "models.generateImages", Signature: "(model: { provider: string; id: string }, context: { input: unknown[] }): Promise<unknown>"},
		)
	}
	return globals
}
