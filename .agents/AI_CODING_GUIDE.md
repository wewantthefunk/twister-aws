# Twister: guide for AI coding assistants

This document tells humans and **AI coding assistants** how to work on **Twister** in a way that matches project intent, keeps quality high, and documents changes for the user.

---

## 1. Purpose of the project

**Twister** is a small **Go** HTTP service that emulates a subset of **real AWS service wire protocols** (SigV4, JSON 1.1, IAM Query form/XML) so clients can point the **AWS CLI or SDK** at Twister with `--endpoint-url` and exercise flows locally (or in controlled environments).

- **In scope today:** **Secrets Manager** and **SSM** (`X-Amz-Target`, JSON 1.1), **S3** path-style REST (scope `s3`: buckets + objects under `s3DataPath/{region}/…`), **IAM** `CreateAccessKey` (form, XML), shared **SigV4**, file-backed data (`dataPath` / env).
- **Not a goal:** full parity with every AWS API, production-grade multi-tenant hardening, or reimplementing the entire AWS surface area unless explicitly asked.

When adding a feature, prefer matching **AWS’s documented** request/response shape where practical so the **official AWS CLI** keeps working with `--endpoint-url`.

---

## 2. Repository layout and patterns

| Area | Pattern |
|------|--------|
| **Entry** | `cmd/twister` — `main` wires `config`, `credentials`, `secretstore`, `paramstore`, `s3buckets`, `awsserver` (`PrimaryHandler` + `Router`), mux (`/` + `/health` + `/refresh`), `net/http.Server`. |
| **Libraries** | `internal/*` — not importable as an external module API; all product logic lives here. |
| **Configuration** | `server.json` + env (see `README.md`). `config.ResolveWithDataPath` for basenames + `dataPath`. |
| **Auth** | `credentials.Provider` + CSV allowlist; `sigv4.Verify` uses the **signing service** in the credential scope (`secretsmanager`, `ssm`, `s3`, `iam`, …). For JSON 1.1, the scope must **match** the `X-Amz-Target` service prefix (after canonicalization). |
| **HTTP routing** | `internal/awsserver.Router` — read body, verify SigV4, then by scope: **IAM** form handler vs **JSON 1.1** `X-Amz-Target` to `awsserver.Service` implementations. |
| **Service modules** | One package per “AWS product” surface (e.g. `internal/secretsmanager`, `internal/ssm`, `internal/iam`); implement `awsserver.Service` where applicable; IAM Query stays separate from JSON 1.1. |
| **Persistence** | Atomic rewrite patterns (temp file + rename) for CSV/JSON; locks where maps are shared (see `credentials.Provider`, `secretstore`, `paramstore`). |
| **Secret files** | **Load order (same for `/refresh`):** `secrets.csv` (primary), then optional **`secrets.json`** (overrides (region, name)), then **seed defaults** for missing demo names. `secrets.json` can be absent. |
| **Parameter files** | Same idea: `parameters.csv` then optional **`parameters.json`** (overrides (region, name)); no seed. See **README** and `paramstore` / `ssm` packages. |
| **Health** | `GET/HEAD /health` — no auth, plain `ok` body for orchestration. |
| **Refresh** | `GET/POST /refresh` — reload credentials, secret store, and parameter store from disk; extend `awsserver.Refresher` when new file-backed state exists. |
| **Container** | `Dockerfile` — multi-stage, static binary, `TWISTER_DATA_PATH=/app`, non-root `twister` user. **`make run`** uses **`mapPath`** from `server.json` (and `go run ./cmd/mappath`) to bind-mount a host path to `/app` — not the repo root by default; see `README.md`. |
| **AI / contributor docs** | This file: `.agents/AI_CODING_GUIDE.md` |

**Adding a new JSON 1.1 service:** implement `awsserver.Service` (`ServiceName`, `Handle`), register in `NewRouter` from `main`, allow the service name in `sigv4` if a new scope is needed, add tests, update `README.md`.

**Adding a new IAM Query action:** extend `internal/iam` and router branching if needed, align XML/errors with AWS docs where reasonable, add tests, update `README.md`.

---

## 3. Coding standards

- **Match existing code** in the same file/package: naming, error handling, comments (short and only when non-obvious), test style, import grouping.
- **Minimize diffs** — do not reformat or refactor unrelated code. One focused change is better than a large cleanup mixed with a feature.
- **Go version** — follow `go.mod` (currently Go 1.22+). Use the standard library where sufficient.
- **Security & crypto** — use `crypto` packages correctly; use constant-time comparison where signatures or secrets are compared; do not log secrets.
- **Dependencies** — avoid new third-party dependencies unless the user explicitly approves; default is stdlib + existing patterns.
- **User-facing docs** — see §5; do not add unsolicited top-level `*.md` files unless the user asked.

---

## 4. Tests (required)

- For **all new or materially changed** Go code, **add or update** tests so `go test ./...` passes.
- Prefer **table-driven** tests for multiple cases; use `httptest` for HTTP; use `t.TempDir()` for file-backed tests.
- Cover success paths, important errors, and security-relevant edge cases (bad signatures, empty allowlist, wrong `Content-Type`, etc. where applicable).
- Run **`make test`** (or `go test ./...`) before finishing; fix failures before handing work back.

---

## 5. README.md

- When a change is **user-visible** (new env var, new endpoint, new CLI example, new Docker/Make target, data file format, or breaking behavior), **update `README.md`** in the same change so users can operate Twister without reading source.
- Keep README accurate with **defaults** (`server.json`, ports, `dataPath`, file names).
- A short **pointer** to this guide appears in `README.md` (Layout table).

---

## 6. Build and run (for verification)

- **Local:** `go run ./cmd/twister` from repo root.
- **Tests:** `make test`
- **Docker image:** `make build`
- **Run container (dev):** `make run` — host path = **`mapPath`** in `server.json` (optional `SERVER_JSON=…` for which file to read), SELinux **`:z`**, and `--user` — see `README.md` and `Makefile` (`make stop`, `IMAGE`, `TAG`, `PORT`, `CONTAINER_NAME`).

---

## 7. When uncertain

- Prefer **clarity and AWS compatibility** over cleverness; when in doubt, follow patterns in `internal/awsserver` and an existing `internal/secretsmanager` or `internal/iam` implementation.
- If a change would require **new** SigV4 scopes or a **new** top-level route, read `internal/sigv4/verify.go` and `internal/awsserver/router.go` first.
