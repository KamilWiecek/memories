# Memories Application

Linear: https://linear.app/6be76fcd8a52/project/memories-application-f0541c7bb3b6
Design docs (architecture, requirements, components): [README.md](README.md)

## Principles

- README.md is the authoritative design reference — respect it
- Seek simplicity; no abstractions beyond what the task requires
- Same k8s manifests must work on both kind (local) and k3s (production)

## Stack

| Layer | Choice |
|---|---|
| Frontend | Plain HTML + JS — no framework, no build step |
| Backend API | Go |
| Database | PostgreSQL |
| Audio storage | PVC (local disk, ReadWriteMany) |
| Speech-to-text | faster-whisper sidecar in CronJob pod |
| Auth | Bearer token — multiple tokens via `AUTH_TOKENS` env var (newline-separated) |
| Local dev | kind cluster named **`memories`** |
| Production | k3s |

## Repository layout

```
k8s/              Kubernetes manifests
  base.yaml       Namespace, PVC, Secret, ConfigMap
  postgres.yaml   StatefulSet + Service
  api.yaml        Deployment + Service + Ingress
  cronjob.yaml    CronJob with Whisper sidecar
cmd/
  api/            Go backend API binary
  cronjob/        Go CronJob binary
static/           Frontend HTML/JS (embedded via go:embed)
whisper/          Dockerfile for faster-whisper sidecar
Makefile
kind-config.yaml
```

## Env vars

| Var | Used by | Notes |
|---|---|---|
| `DATABASE_URL` | API, CronJob | Full Postgres connection string |
| `AUTH_TOKENS` | API | Newline-separated valid Bearer tokens |
| `AUDIO_DIR` | API, CronJob | PVC mount path |
| `PORT` | API | Default `8080` |
| `WHISPER_MODEL` | Whisper sidecar | Default `base` |

## API

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/` | — | Serve embedded frontend |
| GET | `/healthz` | — | Liveness probe, always 200 |
| POST | `/api/upload` | ✓ | `multipart/form-data`, field `audio`; returns created record (201) |
| GET | `/api/memories` | ✓ | All rows ordered `created_at DESC`; omit `audio_path` from response |

## Database schema

```sql
CREATE TYPE memory_status AS ENUM ('pending', 'processing', 'done', 'failed');

CREATE TABLE memories (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  audio_path TEXT NOT NULL,
  transcript TEXT,
  status     memory_status NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON memories (status);
```

## Make targets

```bash
make dev-up      # create kind cluster "memories", load images, apply manifests
make dev-down    # delete the kind cluster
make dev-images  # rebuild + reload images into running cluster
make build       # build all Docker images
make apply       # kubectl apply -f k8s/
```
