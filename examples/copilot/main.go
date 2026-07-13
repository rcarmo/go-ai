// Package main demonstrates GitHub Copilot OAuth, UI model selection, model
// switching, and streaming. Replace loadCredentials/saveCredentials with your
// app's secure storage.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/githubcopilot"
	"github.com/rcarmo/go-ai/oauth"
)

func main() {
	creds := loadCredentials()
	if creds == nil {
		provider := oauth.GetProvider("github-copilot")
		if provider == nil {
			log.Fatal("github-copilot OAuth provider not registered")
		}
		var err error
		creds, err = provider.Login(oauth.LoginCallbacks{
			OnAuth: func(info oauth.AuthInfo) {
				fmt.Println(info.Instructions)
				fmt.Println(info.URL)
			},
			OnPrompt: func(prompt oauth.Prompt) (string, error) {
				fmt.Print(prompt.Message + " ")
				text, err := bufio.NewReader(os.Stdin).ReadString('\n')
				return strings.TrimSpace(text), err
			},
			OnProgress: func(message string) { fmt.Fprintln(os.Stderr, message) },
		})
		if err != nil {
			log.Fatal(err)
		}
		saveCredentials(creds)
	}

	runtime, err := oauth.RuntimeForGitHubCopilot(creds)
	if err != nil {
		log.Fatal(err)
	}
	if runtime.Credentials != creds {
		saveCredentials(runtime.Credentials)
	}

	items := runtime.ModelPickerItems(goai.ProviderGitHubCopilot)
	if len(items) == 0 {
		log.Fatal("no GitHub Copilot models available for this account")
	}
	fmt.Println("Available Copilot models:")
	for i, item := range items {
		fmt.Printf("%2d. %s\n", i+1, item)
	}

	selected := items[0]
	if envSelected := os.Getenv("GOAI_COPILOT_MODEL"); envSelected != "" {
		selected = envSelected
	}
	model, err := runtime.SelectModel(selected)
	if err != nil {
		log.Fatal(err)
	}

	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Say hello from Copilot.")}}
	ctx = runtime.SwitchContextForModel(ctx, model)
	events := goai.Stream(context.Background(), model, ctx, runtime.StreamOptions())
	for event := range events {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			fmt.Print(e.Delta)
		case *goai.ErrorEvent:
			log.Fatal(e.Err)
		}
	}
	fmt.Println()
}

func loadCredentials() *oauth.Credentials {
	// TODO: load from keychain/secret store in a real app.
	return nil
}

func saveCredentials(*oauth.Credentials) {
	// TODO: persist to keychain/secret store in a real app.
}
