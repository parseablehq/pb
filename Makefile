PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
# This is just for development purposes. The version is determined at build time using git tags.
VERSION ?= $(shell git describe --tags 2>/dev/null || echo "dev")
TAG ?= "parseablehq/pb:$(VERSION)"
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go $(VERSION))

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

GOLANGCI_LINT_VERSION := v2.11.4

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@mkdir -p ${GOPATH}/bin
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)"
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin $(GOLANGCI_LINT_VERSION)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps vet lint

docker: build
	@docker build -t $(TAG) . -f Dockerfile.dev

vet:
	@echo "Running $@"
	@go vet $(PWD)/...

lint:
	@echo "Running $@ check"
	@${GOPATH}/bin/golangci-lint run --timeout=5m --config ./.golangci.yml

# Builds pb locally.
build: checks
	@echo "Building pb binary to './pb'"
	@CGO_ENABLED=0 go build -trimpath -tags kqueue --ldflags "$(LDFLAGS)" -o $(PWD)/pb

# Build pb for all supported platforms.
build-release: verifiers crosscompile
	@echo "Built releases for version $(VERSION)" 

# Builds pb and installs it to $GOPATH/bin.
install: build
	@echo "Installing pb binary to '$(GOPATH)/bin/pb'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/pb $(GOPATH)/bin/pb
	@echo "Installation successful. To learn more, try \"pb --help\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf pb
	@rm -rvf build
	@rm -rvf release
