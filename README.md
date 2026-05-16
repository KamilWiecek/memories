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

### Prerequisites

| Tool | Purpose |
|---|---|
| [Docker](https://docs.docker.com/get-docker/) | Build and run container images |
| [kind](https://kind.sigs.k8s.io/) | Local Kubernetes cluster |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Apply manifests and inspect resources |
| [helm](https://helm.sh/docs/intro/install/) | Install nginx ingress controller |
| [make](https://www.gnu.org/software/make/) | Convenience targets |

### First-time setup

**1. Configure secrets**

Edit `k8s/base.yaml` and set the values in `memories-secret` before applying:

- `AUTH_TOKENS` — one Bearer token per line (newline-separated, base64-encoded)
- `DATABASE_URL` — Postgres connection string (defaults work for the in-cluster DB)

**2. Bootstrap the cluster**

```bash
make dev-up
```

This creates a `kind` cluster named **`memories`**, builds all Docker images, loads them into the cluster, and applies every manifest under `k8s/`. The app is available at **http://localhost** when the pods are ready.

### Make targets

| Target | What it does |
|---|---|
| `make dev-up` | Create kind cluster, build + load images, apply all manifests |
| `make dev-down` | Delete the kind cluster |
| `make dev-images` | Rebuild images and reload them into the running cluster |
| `make build` | Build all Docker images locally (no cluster needed) |
| `make apply` | `kubectl apply -f k8s/` against the current cluster |

### Iterating on code

After changing Go or frontend files:

```bash
make dev-images
kubectl rollout restart deployment/memories-api -n memories
```

The CronJob picks up the new image on the next scheduled run; trigger it immediately with:

```bash
kubectl create job -n memories --from=cronjob/memories-cronjob adhoc-$(date +%s)
```

### Accessing the app

Open **http://localhost** in a browser. You will be prompted for a Bearer token — use any value you set in `AUTH_TOKENS` when configuring the secret.

### Troubleshooting

```bash
# Watch pod status
kubectl get pods -n memories -w

# API logs
kubectl logs -n memories deployment/memories-api

# Most recent CronJob run logs
kubectl logs -n memories -l app=memories-cronjob --tail=100

# Connect to Postgres directly (in-cluster exec)
kubectl exec -n memories -it statefulset/memories-postgres -- \
  psql -U memories -d memories

# Connect from your local machine via port-forward
kubectl port-forward -n memories statefulset/memories-postgres 5432:5432 &
psql "postgres://memories:memories@localhost:5432/memories"
# — or with individual flags:
psql -h localhost -p 5432 -U memories -d memories
# Stop the port-forward when done:
kill %1

# Useful queries once connected:
# List all memories
SELECT id, status, created_at, left(transcript, 60) FROM memories ORDER BY created_at DESC;
# Count by status
SELECT status, count(*) FROM memories GROUP BY status;
# Inspect table structure
\d memories
```
