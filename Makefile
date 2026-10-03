# Common commands. Run `make help` to list them.

IMAGE   ?= opscope
VERSION ?= dev

# Docker settings shared by the run targets:
#   - the port is published on 127.0.0.1 only, because Opscope has no login
#   - saved clusters live in the "opscope-data" volume, so they survive restarts
DOCKER_RUN = docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data

.PHONY: help dev-backend dev-frontend test build docker-build docker-run docker-run-env

help:
	@echo "make dev-backend                 run the Go server on :8080"
	@echo "make dev-frontend                run the Vite dev server on :5173 (proxies /api to :8080)"
	@echo "make test                        run the Go tests"
	@echo "make build                       build the frontend and the Go binary locally"
	@echo "make docker-build                build the Docker image ($(IMAGE):$(VERSION))"
	@echo "make docker-run                  run the image; add clusters in the UI"
	@echo "make docker-run-env KUBECONFIG_FILE=path/to/kubeconfig"
	@echo "                                 run the image with a cluster from a kubeconfig file"

# Pass a cluster to the dev server like this:
#   make dev-backend OPSCOPE_KUBECONFIG=$PWD/data/multipass.kubeconfig
# (make passes variables given on the command line to the program as env vars)
dev-backend:
	cd backend && go run .

dev-frontend:
	cd frontend && npm install && npm run dev

test:
	cd backend && go test ./...

build:
	cd frontend && npm ci && npm run build
	cd backend && go build -ldflags "-X main.version=$(VERSION)" -o opscope .

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

docker-run:
	$(DOCKER_RUN) $(IMAGE):$(VERSION)

docker-run-env:
	@test -n "$(KUBECONFIG_FILE)" || (echo "usage: make docker-run-env KUBECONFIG_FILE=path/to/kubeconfig" && exit 1)
	$(DOCKER_RUN) \
		-v "$(abspath $(KUBECONFIG_FILE)):/config/kubeconfig:ro" \
		-e OPSCOPE_KUBECONFIG=/config/kubeconfig \
		$(IMAGE):$(VERSION)
