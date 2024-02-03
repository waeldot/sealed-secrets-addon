BINARY = "addon"

.PHONY: all build test

all: test build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY) .

build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $(BINARY) .

test:
	go test -v ./...
