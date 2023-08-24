PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
VERSION ?= $(shell git describe --tags)
TAG ?= "parseablehq/pb:$(VERSION)"
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go $(VERSION))

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@mkdir -p ${GOPATH}/bin
	@echo "Installing golangci-lint" && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin
	@echo "Installing stringer" && go install -v golang.org/x/tools/cmd/stringer@latest
	@echo "Installing staticheck" && go install honnef.co/go/tools/cmd/staticcheck@latest

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps vet lint

docker: build
	@docker build -t $(TAG) . -f Dockerfile.dev

vet:
	@echo "Running $@"
	@GO111MODULE=on go vet $(PWD)/...

lint:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run --timeout=5m --config ./.golangci.yml
	@GO111MODULE=on ${GOPATH}/bin/staticcheck -tests=false -checks="all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-ST1023,-ST1005" ./...

# Builds pb locally.
build: checks
	@echo "Building pb binary to './pb'"
	@GO111MODULE=on CGO_ENABLED=0 go build -trimpath -tags kqueue --ldflags "$(LDFLAGS)" -o $(PWD)/pb

build-release: checks
	@echo "Building release for version $(VERSION)"
	@(env bash $(PWD)/buildscripts/cross-compile.sh $(VERSION))

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
