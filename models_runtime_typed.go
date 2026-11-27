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
		return Provider(m.Provider) == provider && m.ID != ""
	})
	out.ClassifierModels = copyCatalogModels(entry.ClassifierModels, cloneClassifierModel, func(m *ClassifierModel) bool {
		return Provider(m.Provider) == provider && m.ID != ""
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
