BINARY := bin/oli

.PHONY: build check fmt-check run test test-race vet

build:
	@mkdir -p bin
	go build -trimpath -o $(BINARY) ./cmd/oli

run:
	go run ./cmd/oli

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

check: fmt-check vet test-race build
