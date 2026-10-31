package goai

import (
	"path/filepath"
	"testing"
)

func TestVertexADCCredentialPathPlatforms(t *testing.T) {
	root := t.TempDir()
	home, appdata := filepath.Join(root, "home"), filepath.Join(root, "appdata")
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", appdata)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(root, "ambient.json"))
	for _, tc := range []struct {
		name, goos string
		env        ProviderEnv
		want       string
	}{
		{"Unix scoped home", "linux", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": "", "HOME": home}, filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")},
		{"Darwin default home", "darwin", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": ""}, filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")},
		{"Windows appdata", "windows", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": "", "HOME": "unused", "APPDATA": appdata}, filepath.Join(appdata, "gcloud", "application_default_credentials.json")},
		{"Unix empty suppression", "linux", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": "", "HOME": ""}, ""},
		{"Windows empty suppression", "windows", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": "", "APPDATA": ""}, ""},
		{"explicit precedence", "windows", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": "explicit.json", "APPDATA": ""}, "explicit.json"},
		{"filename whitespace retained", "linux", ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": " "}, " "},
		{"process explicit fallback", "linux", nil, filepath.Join(root, "ambient.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vertexADCCredentialPath(tc.goos, tc.env); got != tc.want {
				t.Errorf("candidate=%q want %q", got, tc.want)
			}
		})
	}
}
