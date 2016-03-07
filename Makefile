export GO15VENDOREXPERIMENT = 1

# Build
build: depends
	go build

# Dependencies
depends:
	glide -q install

update-depends:
	glide -q update

# Run all tests
test: unit

unit:
	go test -v -coverprofile coverage.out
