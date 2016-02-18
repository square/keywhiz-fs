export GO15VENDOREXPERIMENT = 1

# Build
build: depends git-fsck
	go build keywhizfs/main.go

# Dependencies
depends:
	glide -q install

update-depends:
	glide -q update

# Check integrity of dependencies
git-fsck:
	@for repo in `find vendor -name .git`; do \
		echo "git --git-dir=$$repo fsck --full --strict --no-dangling"; \
		git --git-dir=$$repo fsck --full --strict --no-dangling || exit 1; \
	done

# Run all tests
test: unit

# Run unit tests
pre-unit:
	@echo "*** Running unit tests ***"

unit: pre-unit
	go test -v
