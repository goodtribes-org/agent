# Every Go command runs in a container — there is no local Go toolchain by
# design, the same arrangement as postfix-client. The module and build caches
# live in named volumes so a rebuild is not a fresh download.
#
# The container runs as the host user. Without that, everything the toolchain
# writes back into the bind mount — bin/, a formatted source file, a freshly
# generated go.sum — lands owned by root and can no longer be edited from
# outside the container. The cost is that the cache volumes have to be given
# to that user once; cache-init does it.

GO_IMAGE   ?= golang:1.25-alpine
MODCACHE   ?= organizzer-gomod
BUILDCACHE ?= organizzer-gobuild
UID        := $(shell id -u)
GID        := $(shell id -g)

WORKING_ROOT ?= ..

DOCKER_GO = docker run --rm \
	-u $(UID):$(GID) \
	-v "$(CURDIR)":/src -w /src \
	-v $(MODCACHE):/gopath \
	-v $(BUILDCACHE):/gocache \
	-e GOPATH=/gopath -e GOCACHE=/gocache -e HOME=/tmp \
	-e GOFLAGS=-mod=mod -e CGO_ENABLED=0 \
	$(GO_IMAGE) go

.PHONY: help cache-init build test tidy vet fmt lint clean images sync-guides

help:
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

cache-init: ## Create the cache volumes and hand them to the host user (once)
	@docker volume create $(MODCACHE) >/dev/null
	@docker volume create $(BUILDCACHE) >/dev/null
	@docker run --rm -u 0:0 -v $(MODCACHE):/m -v $(BUILDCACHE):/b \
		$(GO_IMAGE) chown -R $(UID):$(GID) /m /b

build: cache-init ## Compile the binary into bin/
	$(DOCKER_GO) build -o bin/ ./cmd/...
	@ls -1 bin/

test: cache-init ## Run the test suite
	$(DOCKER_GO) test ./...

vet: cache-init ## go vet
	$(DOCKER_GO) vet ./...

fmt: cache-init ## Format the tree
	$(DOCKER_GO) fmt ./...

tidy: cache-init ## Sync go.mod / go.sum
	$(DOCKER_GO) mod tidy

lint: vet ## Alias for vet until a linter is pinned

clean: ## Remove build output and the cache volumes
	@docker run --rm -v "$(CURDIR)":/src $(GO_IMAGE) rm -rf /src/bin
	-docker volume rm $(MODCACHE) $(BUILDCACHE)

images: ## Build the image locally
	docker build -t organizzer:dev .

# guides/working-root.md is a copy of the working root's CLAUDE.md, which lives
# in no repository. A stale copy means an invariant silently stops being
# checked, so re-run this whenever the root guide changes.
sync-guides: ## Refresh guides/working-root.md from the working root's CLAUDE.md
	@test -f $(WORKING_ROOT)/CLAUDE.md || { echo "no CLAUDE.md at $(WORKING_ROOT)"; exit 1; }
	cp $(WORKING_ROOT)/CLAUDE.md guides/working-root.md
	@echo "guides/working-root.md updated — commit it"
