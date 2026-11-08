# GoList Makefile

BINARY_NAME=golist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

.PHONY: all build test vet clean docker-build run

all: test build

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME) .

test:
	go test -v -race ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe
	rm -rf dist/

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) -t golist:$(VERSION) -t golist:latest .

run: build
	./$(BINARY_NAME) status
