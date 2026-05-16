# Memories

App for dumping memories, so they never slip away. Records audio in the browser, transcribes it with Whisper, and stores the result in a searchable list.

---

## Architecture

> Interactive diagram: [architecture.drawio](architecture.drawio) — open with [diagrams.net](https://app.diagrams.net)

```
                          Browser
                             │ HTTPS
                             ▼
┌────────────────────── k3s / kind cluster ──────────────────────────┐
│                          [Ingress]                                  │
│                             │                                       │
│                             ▼                                       │
│              ┌──────────────────────────┐   ┌───────────────────┐  │
│              │    Backend API (Go)      │──▶│   k8s Secret      │  │
│              │  • serves frontend HTML  │   │ (tokens, DB creds)│  │
│              │  • POST /api/upload      │   └───────────────────┘  │
│              │  • GET  /api/memories    │                           │
│              │  • Bearer token auth     │                           │
│              └──────┬──────────┬────────┘                          │
│                     │          │                                    │
│                     ▼          ▼                                    │
│              [PostgreSQL]    [PVC]                                  │
│               memories       audio                                  │
│               table          files                                  │
│                     ▲          │                                    │
│                     │          │                                    │
│              ┌─ CronJob Pod ───┼──────────────────┐                │
│              │  [CronJob (Go)] │                  │                │
│              │   reads pending─┘  ──transcribe──▶ [Whisper]       │
│              │   writes transcript                 (sidecar)       │
│              └────────────────────────────────────────────────────┘│
└────────────────────────────────────────────────────────────────────┘
```

---

## Requirements

1. User can record audio directly in the browser via a Record button
2. Recorded audio is uploaded to the backend and stored on a PVC
3. A DB record is created on upload with `status=pending`
4. Main page lists all memories (transcript + date), newest first
5. Memories still being processed show a "processing…" indicator
6. Access requires a Bearer token; multiple valid tokens supported via k8s Secret
7. A CronJob runs periodically and picks up all `pending` records
8. Whisper runs as a sidecar container in the CronJob pod and transcribes audio to text
9. DB record is updated with the transcript and `status=done` (or `failed`)
10. All k8s manifests live in the repo and apply cleanly to k3s with `kubectl apply`
11. A local `kind` cluster named **`memories`** can be bootstrapped with a single command for development
12. Same manifests target both kind (local) and k3s (production); differences handled via env vars or overlays

---

## Components

| Component | Role |
|---|---|
| **Frontend (HTML/JS)** | Single HTML page served by the Go backend. Uses `MediaRecorder` API to capture audio and uploads via `multipart/form-data`. Fetches and renders the memory list. Stores the auth token in `localStorage`. |
| **Backend API (Go)** | Single binary: serves static files + REST endpoints. Writes uploaded audio to the shared PVC and inserts a row in PostgreSQL. Auth middleware validates Bearer tokens against a list loaded from env. |
| **PostgreSQL** | Persists the `memories` table: `id`, `audio_path`, `transcript`, `status` (pending / processing / done / failed), `created_at`, `updated_at`. |
| **PVC** | `ReadWriteMany` PersistentVolumeClaim shared by the API pod and the CronJob pod. Raw audio files are referenced by path in the DB row. |
| **CronJob (Go)** | Runs on a schedule (e.g. every 2 min). Queries DB for `pending` rows, marks them `processing`, feeds each audio file to the Whisper sidecar, then writes the transcript back and sets `done` or `failed`. |
| **Whisper sidecar** | Container running faster-whisper alongside the CronJob. Invoked via CLI exec. Accepts an audio file path, returns the transcript text to stdout. |
| **k8s Secret** | Holds the list of valid auth tokens and PostgreSQL credentials. Mounted as env vars into both the API and the CronJob. |
| **Ingress** | Exposes the backend API to the network via a hostname. Handles TLS termination. |

---

## Local Development

Requirements: [Docker](https://docs.docker.com/get-docker/), [kind](https://kind.sigs.k8s.io/), [kubectl](https://kubernetes.io/docs/tasks/tools/)

```bash
make dev-up    # create kind cluster "memories" and apply all manifests
make dev-down  # tear it down
```

See `Makefile` for all available targets.
