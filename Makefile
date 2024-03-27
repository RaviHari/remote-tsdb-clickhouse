BINARY := remote-tsdb-clickhouse
PLATFORMS := linux/amd64,linux/arm64
IMG ?= docker.intuit.com/personal/rhari/remote-tsdb-clickhouse:latest
PKGS := $(shell go list ./... | grep -v mocks)
GIT_COMMIT := $(shell git rev-parse --short HEAD)
GIT_BRANCH := $(shell git branch --show-current)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_ARCH := $(shell go env GOARCH)
BUILD_OS   := $(shell go env GOOS)
SLASH:= /
DASH:= -
# replace . with -
GIT_BRANCH_DASH= $(subst $(SLASH),$(DASH),$(GIT_BRANCH))

# git describe --exact-match --abbrev=0 will get the most recent local tag.
# Otherwise, a default semver version is generated for local builds.
override GIT_TAG = $(shell git describe --exact-match --abbrev=0  2> /dev/null || echo v0.0.0-$(GIT_BRANCH_DASH)-$(GIT_COMMIT))

override LDFLAGS += \
	-X 'github.intuit.com/kubernetes/cost-monitor/pkg/version.GitCommit=${GIT_COMMIT}' \
	-X 'github.intuit.com/kubernetes/cost-monitor/pkg/version.BuildDate=${BUILD_DATE}' \
	-X 'github.intuit.com/kubernetes/cost-monitor/pkg/version.Version=$(GIT_TAG)'

.PHONY: lint
lint: check-linter
	golangci-lint run --tests --fix ./...

.PHONY: check-linter
check-linter:
ifeq (, $(shell which golangci-lint))
	$(error "golangci-lint not found. https://github.com/golangci/golangci-lint")
else
	@echo "golangci-lint found in $(shell which golangci-lint)"
endif

.PHONY: fmt
fmt:
	go fmt ${PKGS}

# Run go vet against code
.PHONY: vet
vet:
	go vet ${PKGS}

generate:
	go generate ${PKGS}

.PHONY: test
test: generate fmt vet
	go test -race -coverprofile coverage.out -ldflags "${LDFLAGS} -extldflags -static" ${PKGS}

.PHONY: cover
cover: test
	go tool cover -func=coverage.out -o coverage.txt
	go tool cover -html=coverage.out -o coverage.html
	@cat coverage.txt
	@echo "Run 'open coverage.html' to view coverage report."


docker-build: dist/$(BINARY)-linux-amd64
	docker build -t ${IMG} .

.PHONY: clean
clean:
	@echo "Removing package object files..."
	@go clean ./...
	@echo "Removing testcache and coverage results..."
	@go clean -testcache
	@rm -f coverage.*
	@echo "Removing generated binaries and vendor libs..."
	@rm -rf dist

.PHONY: build
build: clean dist/$(BINARY)-$(BUILD_OS)-$(BUILD_ARCH)
	mv dist/$(BINARY)-$(BUILD_OS)-$(BUILD_ARCH) dist/$(BINARY)

dist/$(BINARY)-%-amd64: BUILD_ARCH=amd64
dist/$(BINARY)-%-arm64: BUILD_ARCH=arm64

dist/$(BINARY)-linux-%: fmt vet
	CGO_ENABLED=0 GOOS=linux GOARCH=$(BUILD_ARCH) go build -ldflags "${LDFLAGS} -extldflags -static" -o $@

dist/$(BINARY)-darwin-%: fmt vet
	GOOS=darwin GOARCH=$(BUILD_ARCH) go build -ldflags "${LDFLAGS}" -o $@

dist: dist/$(BINARY)-linux-amd64 dist/$(BINARY)-linux-arm64 dist/$(BINARY)-darwin-amd64 dist/$(BINARY)-darwin-arm64

artifactory-upload: dist
	./hack/artifactory-upload.sh dist $(GIT_TAG)

github-release: dist
	./hack/github-release.sh dist $(GIT_TAG)