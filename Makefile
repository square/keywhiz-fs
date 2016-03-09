export GO15VENDOREXPERIMENT = 1
REVISION := $(shell git describe --long --always --abbrev=8 HEAD)

# Build
build: depends
	go build -ldflags "-X main.VERSION='2.0-$(REVISION)'"

# Dependencies
depends:
	glide -q install

update-depends:
	glide -q update

# Run all tests
test: unit

unit:
	go test -v -coverprofile coverage.out
