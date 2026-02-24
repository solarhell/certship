# See https://tech.davis-hansson.com/p/make/
SHELL := bash
.DELETE_ON_ERROR:
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := build
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-print-directory

GO_MODULE_NAME = github.com/solarhell/certship
VERSION_FLAG=-X '$(GO_MODULE_NAME)/internal/version.gitBranch=`git branch --show-current`' \
-X '$(GO_MODULE_NAME)/internal/version.gitCommit=`git rev-parse HEAD`' \
-X '$(GO_MODULE_NAME)/internal/version.gitTag=`git describe --always`' \
-X '$(GO_MODULE_NAME)/internal/version.buildUser=`whoami`' \
-X '$(GO_MODULE_NAME)/internal/version.buildDate=`date +'%Y-%m-%dT%H:%M:%SZ'`'

GO_LDFLAGS=-ldflags "-s $(VERSION_FLAG)"

.PHONY: build
build:
	rm -rf ./dist
	GOOS=linux GOARCH=amd64 go build $(GO_LDFLAGS) -o ./dist/certship-linux-amd64 ./cmd/certship
	GOOS=darwin GOARCH=arm64 go build $(GO_LDFLAGS) -o ./dist/certship-darwin-arm64 ./cmd/certship

.PHONY: proto
proto:
	buf generate
	go mod tidy -v

.PHONY: ent
ent:
	go generate ./pkg/entgenerate
	go mod tidy -v

.PHONY: lint
lint:
	go mod tidy -v
	goimports-reviser -format -set-alias -project-name $(GO_MODULE_NAME) -rm-unused ./...
	go fix ./...
	go vet ./...
	golangci-lint run --fix
	staticcheck ./...

.PHONY: build-local
build-local:
	go build -o ./certship ./cmd/certship

.PHONY: init
init:
	go install github.com/incu6us/goimports-reviser/v3@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install entgo.io/ent/cmd/ent@v0.14.5
	brew install golangci-lint
