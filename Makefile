# Include go binaries into path
export PATH := $(GOPATH)/bin:$(PATH)

CURDIR := $(shell pwd)
GOBIN := $(CURDIR)/bin/
ENV:=GOBIN=$(GOBIN)

# full cleaning. Don't use it: it removes outside golang packages for all projects
clean: ## Remove build artifacts
	@echo "======================================================================"
	@echo "Run clean"
	go clean -i -r -x -cache -testcache -modcache

clean-cache: ## Clean golang cache
	@echo "clean-cache started..."
	go clean -cache
	go clean -testcache
	@echo "clean-cache complete!"

clean-vendor: ## Remove vendor folder
	@echo "clean-vendor started..."
	rm -fr ./vendor
	@echo "clean-vendor complete!"

clean-all: clean clean-vendor clean-cache

mod-action-%:
	@echo "Run go mod ${*}...."
	GO111MODULE=on go mod $*
	@echo "Done go mod  ${*}"

mod: mod-action-verify mod-action-tidy mod-action-vendor mod-action-download mod-action-verify ## Download all dependencies

test: ## Testing
	@echo "======================================================================"
	@echo "Run test"
	rm -f coverage.out coverage.html
	go test -cover -race -coverprofile=$(PWD)/coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	rm coverage.out