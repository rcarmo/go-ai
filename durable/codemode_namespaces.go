package durable

func validateCodemodeNamespaces(namespaces []CodemodeNamespace, l Limits) error {
	if len(namespaces) > l.MaxPage {
		return reject("codemode namespace budget")
	}
	names := map[string]bool{}
	identifiers := map[string]bool{}
	prefixes := map[string]bool{}
	for _, ns := range namespaces {
		identifier := CodemodeIdentifier(ns.Name)
		if ns.Name == "" || ns.Prefix == "" || len(ns.Name) > l.MaxStringBytes || len(ns.Prefix) > l.MaxStringBytes || len(ns.Description) > l.MaxStringBytes || len(ns.Instructions) > l.MaxStringBytes || names[ns.Name] || identifiers[identifier] || prefixes[ns.Prefix] {
			return reject("invalid codemode namespace")
		}
		names[ns.Name] = true
		identifiers[identifier] = true
		prefixes[ns.Prefix] = true
	}
	return nil
}
