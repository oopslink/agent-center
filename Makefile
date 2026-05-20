.PHONY: build test cover lint vet tidy clean

# Phase 1: build / test / cover targets

BIN := agent-center

build:
	go build -o ./bin/$(BIN) ./cmd/agent-center

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

cover-html: cover
	go tool cover -html=coverage.out -o coverage.html

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf ./bin coverage.out coverage.html
