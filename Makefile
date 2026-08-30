BINARY := bin/mac-cleanup-studio

.PHONY: build check fmt-check run test test-race vet

build:
	@mkdir -p bin
	go build -trimpath -o $(BINARY) ./cmd/mac-cleanup-studio

run:
	go run ./cmd/mac-cleanup-studio

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

check: fmt-check vet test-race build
