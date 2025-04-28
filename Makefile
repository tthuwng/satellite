.PHONY: run test clean fmt vet build all test-verbose

BINARY_NAME=satellite

all: build

run:
	go run ./cmd/satellite/main.go -- $(ARGS)

test:
	go test ./...

build:
	go build -o $(BINARY_NAME) ./cmd/satellite/main.go

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)

test-verbose:
	go test -v ./...