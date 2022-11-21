BINARY = "addon"
VERSION = "1.0"
GO_LD_FLAGS = "-X main.VERSION=$(VERSION)"

.PHONY: all build test

all: test build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY) -ldflags $(GO_LD_FLAGS) .

build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $(BINARY) -ldflags $(GO_LD_FLAGS) .

test:
	go test -v ./...
