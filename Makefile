.PHONY: run test clean fmt vet build all

BINARY_NAME=satellite

all: build

run:
	go run main.go

test:
	go test ./...

build:
	go build -o $(BINARY_NAME) main.go

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)