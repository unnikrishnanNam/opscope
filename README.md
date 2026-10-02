# OpScope

A small, read-only Kubernetes dashboard. Go backend, React frontend, one Docker image.

The project is built in phases; see [docs/PHASES.md](docs/PHASES.md) for the plan and progress.

## Layout

```
backend/                Go server
  main.go               entry point: reads env vars, starts the server
  internal/server/      HTTP routes, middleware, static file serving
frontend/               React app (Vite, plain JavaScript)
  src/sections.js       list of pages; drives the sidebar and the routes
  src/components/       shared pieces (sidebar, top bar, badges)
  src/pages/            one file per page
  src/styles.css        all styles; design tokens at the top
Dockerfile              builds the single image
Makefile                common commands
docs/PHASES.md          build plan and progress
```

## Requirements

- Go 1.26+
- Node.js 24+
- Docker (for the image)

## Run in development

Two terminals:

```bash
make dev-backend
```

```bash
make dev-frontend
```

Open http://localhost:5173. Vite reloads the page when you edit frontend files and forwards
`/api/*` calls to the Go server on port 8080. Restart `make dev-backend` after Go changes.

## Run with Docker

```bash
make docker-build
```

```bash
make docker-run
```

Open http://localhost:8080.

## Configuration

| Variable     | Default             | What it does                          |
| ------------ | ------------------- | ------------------------------------- |
| `PORT`       | `8080`              | Port the server listens on            |
| `STATIC_DIR` | `../frontend/dist`  | Folder with the built React app       |

## API

| Method | Path          | Returns                                   |
| ------ | ------------- | ----------------------------------------- |
| GET    | `/api/health` | `{"status":"ok","version":"..."}`         |
