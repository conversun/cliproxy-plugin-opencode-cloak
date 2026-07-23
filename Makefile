.DEFAULT_GOAL := build

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
	EXT := dylib
else ifeq ($(UNAME_S),Linux)
	EXT := so
else
	EXT := dll
endif

.PHONY: build test vet fmt

build:
	mkdir -p bin
	go build -buildmode=c-shared -o bin/opencode-cloak.$(EXT) .

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .
