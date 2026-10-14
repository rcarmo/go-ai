package durable

var codemodeStoreToken = func() *DocumentDefinition {
	definition, err := DefineDocument(DefinitionOptions{Kind: "pi.codemode.store", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"values": JSON{}}, nil }})
	if err != nil {
		panic(err)
	}
	return definition
}()

func codemodeStoreDefinition() *DocumentDefinition { return codemodeStoreToken }

func validateCodemodeStorePatch(result CodemodeResult, l Limits) error {
	if len(result.StoreDeletes) > l.MaxPage {
		return reject("codemode store deletion budget")
	}
	seen := map[string]bool{}
	for _, key := range result.StoreDeletes {
		if len(key) > l.MaxStringBytes || seen[key] {
			return reject("invalid codemode store deletion")
		}
		if _, ok := result.StoreWrites[key]; ok {
			return reject("conflicting codemode store patch")
		}
		seen[key] = true
	}
	return nil
}
