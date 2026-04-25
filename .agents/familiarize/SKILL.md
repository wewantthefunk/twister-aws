---
name: familiarize
description: >-
  Onboards an AI coding agent by reading repository documentation and high-signal
  code paths to build accurate mental context before implementing or answering.
  Use when starting work in an unfamiliar repo, when the user says familiarize,
  onboard, ramp up, get context, or asks to understand the codebase first.
---

# Familiarize (repo context)

## Discovery (this repo)

| Consumer | Location |
|----------|----------|
| **Canonical skill file** | `.agents/familiarize/SKILL.md` |
| **Cursor** (project skill) | `.cursor/skills/familiarize` → symlink to `.agents/familiarize` |
| **Codex** | Root [`AGENTS.md`](../../AGENTS.md) instructs loading this skill |
| **Claude** (e.g. Claude Code) | Root [`CLAUDE.md`](../../CLAUDE.md) instructs loading this skill |

## Goal

Before writing code or giving architectural answers, **establish grounded context** from this repository: what it does, how it is laid out, how to build and test, and where changes usually go. Prefer **reading files with tools** over guessing from general knowledge.

## When to run

- First message in a session about this repo, or the user explicitly wants a **read-in** / **ramp-up**.
- The task touches multiple packages and you are not already sure of entry points and conventions.

## Workflow

1. **Discover documentation (read first, in order)**  
   If a path does not exist, skip it.
   - `.agents/AI_CODING_GUIDE.md` or `.agents/*.md` — project-specific rules for contributors and AI assistants.
   - `README.md`, `CONTRIBUTING.md`, `docs/` (any index or overview).
   - `AGENTS.md`, `CLAUDE.md`, `.cursor/rules/` or `RULE.md` if present — editor/agent instructions.
   - Root config that explains behavior: e.g. `Makefile`, `docker-compose.yml`, `package.json`, `go.mod`, `pyproject.toml`, `Cargo.toml`.

2. **Map the codebase (targeted reads, not full-tree dumps)**  
   - **Entry points:** e.g. `cmd/`, `src/main.*`, `app/`, `main.go`, `index.ts`.
   - **Core libraries:** e.g. `internal/`, `lib/`, `pkg/`, `src/` (follow imports from entry).
   - **Infra / deploy:** `Dockerfile`, `terraform/`, `k8s/`, CI under `.github/workflows/` (names only first; open files relevant to the task).

3. **Security and hygiene**  
   - Do **not** read or paste contents of obvious secrets: `*.pem`, `id_rsa`, `.env` with real credentials, `credentials` files unless the user asked to debug them. Treat `data/credentials.csv`-style paths as sensitive unless the task requires it.

4. **Synthesize (keep internal or brief for the user)**  
   - **Purpose** in one short paragraph.  
   - **Layout:** entry command(s), main packages/modules, where config and persistence live.  
   - **How to verify:** test/build commands from docs or standard files (`make test`, `go test ./...`, etc.).  
   - **Conventions:** testing style, error handling, doc update expectations (from project docs).

5. **Optional: confirm with the user**  
   If the task scope is ambiguous, one clarifying question after the read-in is better than a wrong assumption.

## This repository (Twister)

When working in **twister-aws**, prioritize:

| Priority | Path | Why |
|----------|------|-----|
| 1 | `.agents/AI_CODING_GUIDE.md` | Intent, layout table, AWS-emulation scope, patterns |
| 2 | `README.md` | User-visible behavior, env, examples |
| 3 | `cmd/twister` | `main` wiring |
| 4 | `internal/` | Product logic by service area |
| 5 | `Makefile` | `test`, `build`, `run` |

Then drill into the specific `internal/*` package that matches the user’s request.

## Anti-patterns

- Skipping README and project agent/guide files when they exist.
- Reading dozens of unrelated files “just in case” — stay **task-adjacent** after the initial map.
- Assuming AWS, language, or framework versions; take them from **repo files** (`go.mod`, `README`, CI).

## Checklist (copy for tracking)

```
Familiarize:
- [ ] Contributor / AI guide under .agents or docs
- [ ] README (and CONTRIBUTING if present)
- [ ] Build & test entry (Makefile, CI, package scripts)
- [ ] Entry point(s) and one level of core modules
- [ ] Notes: purpose, layout, verify command, conventions
```
