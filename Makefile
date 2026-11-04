SHELL := /bin/bash
PROJECT_ID ?= PROJECT_ID
REGION ?= us-west4
ZONE ?= us-west4-a
CLUSTER_NAME ?= gke-model-admission-demo
IMAGE_REPO ?= $(REGION)-docker.pkg.dev/$(PROJECT_ID)/models/gke-model-admission-controller
IMAGE_TAG ?= v0.1.0

.PHONY: all test test-all-repos build lint check helm-lint helm-template terraform-validate tf-fmt test-e2e-gke

all: test build

test:
	go test -race -v ./...

test-all-repos:
	cd ../llm-release-gate && go test -race ./...
	cd ../ai-model-provenance-gate && go test -race ./...
	go test -race -v ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/manager ./cmd/manager
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/modelctl ./cmd/modelctl
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/gke-e2e ./cmd/gke-e2e

lint:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted with gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	fi

check: lint test build helm-lint terraform-validate

helm-lint:
	helm lint ./charts/model-admission-controller
	helm template model-admission-controller ./charts/model-admission-controller --namespace model-admission-system > /dev/null

helm-template:
	helm template model-admission-controller ./charts/model-admission-controller --namespace model-admission-system

tf-fmt:
	terraform -chdir=terraform fmt -check -recursive

terraform-validate: tf-fmt
	terraform -chdir=terraform validate

test-e2e-gke: build
	./bin/gke-e2e \
		--registry-host $(REGION)-docker.pkg.dev \
		--repo-prefix $(PROJECT_ID)/models \
		--runtime-image "$(RUNTIME_IMAGE)"
