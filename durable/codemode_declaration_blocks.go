package durable

import "strings"

// CodemodeGlobalDeclaration is descriptive metadata, never executable authority.
type CodemodeGlobalDeclaration struct {
	Name, Description, Signature string
	InputSchema, OutputSchema    JSON
}

func codemodeDocComment(text, indent string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "*/", "*\\/"), "\n")
	if len(lines) == 1 {
		return []string{indent + "/** " + lines[0] + " */"}
	}
	out := []string{indent + "/**"}
	for _, line := range lines {
		out = append(out, indent+" * "+line)
	}
	return append(out, indent+" */")
}
func codemodeGlobalSignature(global CodemodeGlobalDeclaration) string {
	if signature := strings.TrimSpace(global.Signature); signature != "" {
		return signature
	}
	tool := CodemodeToolDescription{Name: "global", InputSchema: global.InputSchema, OutputSchema: global.OutputSchema}
	signature := RenderCodemodeToolSignature(tool)
	return strings.TrimSuffix(strings.TrimPrefix(signature, "global"), ";")
}

// RenderCodemodeDeclarations emits ordered tools, plain globals, then ordered
// namespace groups. Schema rendering uses the native bounded renderer.
func RenderCodemodeDeclarations(tools []CodemodeToolDescription, globals []CodemodeGlobalDeclaration) string {
	lines := []string{}
	if len(tools) > 0 {
		lines = append(lines, "declare const tools: {")
		for _, tool := range tools {
			lines = append(lines, codemodeDocComment(tool.Description, "  ")...)
			lines = append(lines, "  "+strings.ReplaceAll(RenderCodemodeToolSignature(tool), "\n", "\n  "))
		}
		lines = append(lines, "};")
	}
	namespaces := []string{}
	groups := map[string][]CodemodeGlobalDeclaration{}
	for _, global := range globals {
		namespace, member, grouped := strings.Cut(global.Name, ".")
		if grouped {
			if _, ok := groups[namespace]; !ok {
				namespaces = append(namespaces, namespace)
			}
			global.Name = member
			groups[namespace] = append(groups[namespace], global)
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, codemodeDocComment(global.Description, "")...)
		lines = append(lines, "declare function "+CodemodeIdentifier(global.Name)+codemodeGlobalSignature(global)+";")
	}
	for _, namespace := range namespaces {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "declare const "+CodemodeIdentifier(namespace)+": {")
		for _, global := range groups[namespace] {
			lines = append(lines, codemodeDocComment(global.Description, "  ")...)
			lines = append(lines, "  "+CodemodeIdentifier(global.Name)+strings.ReplaceAll(codemodeGlobalSignature(global), "\n", "\n  ")+";")
		}
		lines = append(lines, "};")
	}
	return strings.Join(lines, "\n")
}
