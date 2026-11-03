// Compare every public native model record with the official offline catalogue.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

func main() {
	dir := flag.String("data-dir", "", "official pi-ai providers/data directory")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "-data-dir is required")
		os.Exit(2)
	}
	if err := compare(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func compare(dir string) error {
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinImageModels()
	goai.RegisterBuiltinClassifierModels()
	native := map[string]any{}
	for _, m := range goai.ListModels("") {
		native["chat/"+string(m.Provider)+"/"+m.ID] = m
	}
	for _, m := range goai.ListImageModels("") {
		native["image/"+string(m.Provider)+"/"+m.ID] = m
	}
	for _, m := range goai.ListClassifierModels("") {
		native["classifier/"+string(m.Provider)+"/"+m.ID] = m
	}
	// Manifest membership is checked by generate-models; read it independently
	// here to compare full records rather than its projected modelEntry fields.
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(dir, ".manifest.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	counts := map[string]int{}
	for file := range manifest.Files {
		if filepath.Base(file) != file || !strings.HasSuffix(file, ".json") {
			return fmt.Errorf("invalid catalogue filename: %q", file)
		}
		provider := strings.TrimSuffix(file, ".json")
		var apis map[string]map[string]map[string]any
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, &apis); err != nil {
			return err
		}
		for api, models := range apis {
			for sourceKey, want := range models {
				kind, _ := want["type"].(string)
				id, _ := want["id"].(string)
				if id == "" || sourceKey != kind+":"+id || want["api"] != api || want["provider"] != provider {
					return fmt.Errorf("invalid source identity: %s/%s/%s", provider, api, sourceKey)
				}
				key := kind + "/" + provider + "/" + id
				model, ok := native[key]
				if !ok {
					return fmt.Errorf("native record missing: %s", key)
				}
				encoded, err := json.Marshal(model)
				if err != nil {
					return err
				}
				var got map[string]any
				if err = json.Unmarshal(encoded, &got); err != nil {
					return err
				}
				// Public native chat records omit their default type. All other
				// fields, including provider/API/ID, must match the source unchanged.
				if got["type"] == nil {
					got["type"] = "chat"
				}
				// Go exposes the source compat union as four typed fields.
				// Collapse exactly one active field without changing its contents.
				for _, field := range []string{"completionsCompat", "responsesCompat", "anthropicCompat", "bedrockCompat"} {
					if value, ok := got[field]; ok {
						if got["compat"] != nil {
							return fmt.Errorf("multiple native compat variants: %s", key)
						}
						got["compat"] = value
						delete(got, field)
					}
				}
				if !reflect.DeepEqual(got, want) {
					a, _ := json.Marshal(want)
					b, _ := json.Marshal(got)
					return fmt.Errorf("full record differs: %s\nsource: %s\nnative: %s", key, a, b)
				}
				counts[kind]++
				delete(native, key)
			}
		}
	}
	if len(native) != 0 {
		return fmt.Errorf("%d extra native catalogue records", len(native))
	}
	fmt.Printf("Full public catalogue records match: chat=%d image=%d classifier=%d\n", counts["chat"], counts["image"], counts["classifier"])
	return nil
}
