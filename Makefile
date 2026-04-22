.PHONY: build test generate clean lint

build:
	go build ./...

test:
	go test ./... -count=1

generate:
	bun run scripts/generate-models.ts

lint:
	golangci-lint run ./...

clean:
	go clean ./...

all: generate build test
