package goai

func copyCatalogModels[T any](models []*T, clone func(*T) *T, keep func(*T) bool) []*T {
	if models == nil {
		return nil
	}
	out := make([]*T, 0, len(models))
	for _, model := range models {
		if model != nil && (keep == nil || keep(model)) {
			out = append(out, clone(model))
		}
	}
	return out
}

func filterProviderCatalog(provider Provider, entry *ModelsStoreEntry) *ModelsStoreEntry {
	out := *entry
	out.Models = filterProviderModels(provider, entry.Models)
	out.ImageModels = copyCatalogModels(entry.ImageModels, cloneImageModel, func(m *ImageModel) bool {
		return Provider(m.Provider) == provider && m.ID != "" && GetModelType(m) == ModelTypeImage
	})
	out.ClassifierModels = copyCatalogModels(entry.ClassifierModels, cloneClassifierModel, func(m *ClassifierModel) bool {
		return Provider(m.Provider) == provider && m.ID != "" && GetModelType(m) == ModelTypeClassifier
	})
	return &out
}

// RuntimeImageCatalogs returns detached image snapshots from the default
// runtime. A present key, even with no models, supersedes that provider's
// legacy registry records. An empty provider selects all typed catalogs.
// Compatibility packages use this read bridge without registering models.
func RuntimeImageCatalogs(provider ImageProvider) map[ImageProvider][]*ImageModel {
	modelRuntimeMu.RLock()
	r := defaultRuntime
	modelRuntimeMu.RUnlock()
	return r.imageCatalogs(provider)
}

func (r *ModelRuntime) imageCatalogs(provider ImageProvider) map[ImageProvider][]*ImageModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[ImageProvider][]*ImageModel{}
	for id, entry := range r.catalogs {
		if entry.ImageModels != nil && (provider == "" || ImageProvider(id) == provider) {
			out[ImageProvider(id)] = copyCatalogModels(entry.ImageModels, cloneImageModel, nil)
		}
	}
	return out
}

func runtimeClassifierCatalogs(provider ClassifierProvider) map[ClassifierProvider][]*ClassifierModel {
	modelRuntimeMu.RLock()
	r := defaultRuntime
	modelRuntimeMu.RUnlock()
	return r.classifierCatalogs(provider)
}

func (r *ModelRuntime) classifierCatalogs(provider ClassifierProvider) map[ClassifierProvider][]*ClassifierModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[ClassifierProvider][]*ClassifierModel{}
	for id, entry := range r.catalogs {
		if entry.ClassifierModels != nil && (provider == "" || ClassifierProvider(id) == provider) {
			out[ClassifierProvider(id)] = copyCatalogModels(entry.ClassifierModels, cloneClassifierModel, nil)
		}
	}
	return out
}

// GetModelsOfType returns detached snapshots for the requested public model
// type. Empty provider selects every registered provider, matching the native
// package-level catalog reads without exposing runtime-owned mutable pointers.
func (r *ModelRuntime) GetModelsOfType(typ ModelType, provider string) []any {
	switch typ {
	case ModelTypeChat:
		models := r.GetModels(Provider(provider))
		out := make([]any, 0, len(models))
		for _, model := range models {
			out = append(out, model)
		}
		return out
	case ModelTypeImage:
		catalogs := r.imageCatalogs(ImageProvider(provider))
		var out []any
		for _, models := range catalogs {
			for _, model := range models {
				if GetModelType(model) == ModelTypeImage {
					out = append(out, model)
				}
			}
		}
		return out
	case ModelTypeClassifier:
		catalogs := r.classifierCatalogs(ClassifierProvider(provider))
		var out []any
		for _, models := range catalogs {
			for _, model := range models {
				if GetModelType(model) == ModelTypeClassifier {
					out = append(out, model)
				}
			}
		}
		return out
	default:
		return nil
	}
}

// GetModelOfType performs a provider-scoped lookup without falling through to
// another model type. Upstream permits one provider/id at each operation type.
func (r *ModelRuntime) GetModelOfType(typ ModelType, provider, id string) any {
	for _, model := range r.GetModelsOfType(typ, provider) {
		ref, ok := typedModelIdentity(model)
		if ok && ref.ID == id && ref.Provider == provider {
			return model
		}
	}
	return nil
}

// GetAllModels returns detached snapshots from all known model types.
func (r *ModelRuntime) GetAllModels(provider string) []any {
	out := make([]any, 0)
	for _, typ := range []ModelType{ModelTypeChat, ModelTypeImage, ModelTypeClassifier} {
		out = append(out, r.GetModelsOfType(typ, provider)...)
	}
	return out
}

// GetImageModel returns a provider-scoped image from this runtime only.
func (r *ModelRuntime) GetImageModel(provider ImageProvider, id string) *ImageModel {
	for _, m := range r.imageCatalogs(provider)[provider] {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// GetClassifierModel returns a provider-scoped classifier from this runtime only.
func (r *ModelRuntime) GetClassifierModel(provider ClassifierProvider, id string) *ClassifierModel {
	for _, m := range r.classifierCatalogs(provider)[provider] {
		if m.ID == id {
			return m
		}
	}
	return nil
}
