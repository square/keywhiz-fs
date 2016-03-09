export GO15VENDOREXPERIMENT = 1

BUILD_TIME := $(shell date +%s)
BUILD_REVISION := $(shell git rev-parse --verify HEAD)
BUILD_MACHINE := $(shell uname -mnrs)

# Build
build: depends
	go build -ldflags " \
	  -X \"main.buildTime=$(BUILD_TIME)\" \
	  -X \"main.buildRevision=$(BUILD_REVISION)\" \
	  -X \"main.buildMachine=$(BUILD_MACHINE)\""

# Dependencies
depends:
	glide -q install

update-depends:
	glide -q update

# Run all tests
test: unit

unit:
	go test -v -coverprofile coverage.out
