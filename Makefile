# Common commands. Run `make help` to list them.

IMAGE   ?= opscope
VERSION ?= dev

.PHONY: help dev-backend dev-frontend test build docker-build docker-run

help:
	@echo "make dev-backend    run the Go server on :8080"
	@echo "make dev-frontend   run the Vite dev server on :5173 (proxies /api to :8080)"
	@echo "make test           run the Go tests"
	@echo "make build          build the frontend and the Go binary locally"
	@echo "make docker-build   build the Docker image ($(IMAGE):$(VERSION))"
	@echo "make docker-run     run the Docker image on http://localhost:8080"

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
	docker run --rm -p 8080:8080 $(IMAGE):$(VERSION)
