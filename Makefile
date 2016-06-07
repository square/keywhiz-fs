# Required for os/user to work on cross-compile
export CGO_ENABLED = 1

BUILD_TIME := $(shell date +%s)
BUILD_REVISION := $(shell git rev-parse --verify HEAD)
BUILD_MACHINE := $(shell uname -mnrs)

SOURCE_FILES := $(shell find . \( -name '*.go' -not -path './vendor/*' \))

# Build
keywhiz-fs: $(SOURCE_FILES)
	go build -ldflags "-s -w \
	  -X \"main.buildTime=$(BUILD_TIME)\" \
	  -X \"main.buildRevision=$(BUILD_REVISION)\" \
	  -X \"main.buildMachine=$(BUILD_MACHINE)\""

# Run all tests
test:
	go test -v -coverprofile coverage.out

.PHONY: test
