.PHONY: run test clean fmt vet build all test-verbose viz view smoke-test

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

viz:                       ## Convert newest graph-*.json -> graph.json
	./viz.sh

view: viz                  ## viz + start a quick web server on :8080
	python3 -m http.server 8080
	@echo "Open http://localhost:8080/viz.html"

smoke-test:
	./smoke_test.sh
