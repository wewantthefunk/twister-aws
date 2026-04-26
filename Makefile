# Twister — Docker + tests (see .agents/AI_CODING_GUIDE.md for project patterns and AI assistant instructions)
#   make build   — build the image
#   make run     — run detached; host path from server.json `mapPath` → /app (see cmd/mappath)
#   make stop    — stop any **running** container created from this image (IMAGE:TAG)
#   make test    — run Go unit tests (go test ./...)
#
# `mapPath` in server.json (override file with SERVER_JSON=…): host directory bind-mounted to /app.
# Example: "mapPath": "data" relative to the directory you run `make` from, or an absolute path.
#
# Note: `id` and volume `:z` are expanded in the shell when the recipe runs (not at Makefile parse time),
# so the container always gets a real --user uid:gid. On SELinux, :z is required to read the bind mount.

IMAGE ?= twister
TAG   ?= latest
PORT  ?= 8080
# Human-readable `docker ps` name for `make run` (`make stop` uses the image, not the name)
CONTAINER_NAME ?= twister
# Config file to read for mapPath (same keys as twister; env TWISTER_SERVER_CONFIG in-container is separate)
SERVER_JSON ?= server.json
# Twister IAM endpoint for `make initial` (override if not localhost:$(PORT))
TWISTER_ENDPOINT ?= http://localhost:$(PORT)

SHELL := /bin/bash

.PHONY: build run stop test initial
build:
	docker build -t $(IMAGE):$(TAG) .

test:
	go test ./...

# go run ./cmd/mappath prints absolute path from `mapPath` in SERVER_JSON. :z = SELinux relabel.
run:
	@P=$$(go run ./cmd/mappath -config "$(SERVER_JSON)"); \
	docker run -d --rm --name $(CONTAINER_NAME) -p $(PORT):8080 -v "$$P:/app:z" --user "$$(id -u):$$(id -g)" $(IMAGE):$(TAG)

# Stop by image so it works for `--name` (twister) and auto names (e.g. jolly_hellman)
stop:
	@ids=$$(docker ps -q --filter ancestor=$(IMAGE):$(TAG)); \
	if [ -n "$$ids" ]; then docker stop $$ids; fi

# Create an access key on Twister and print exports. Load into your shell with:
#   eval "$(make initial)"
# Bootstrap flow: remove local credentials files, sign with temporary credentials,
# refresh Twister state, then create a fresh access key pair on Twister.
# Note: exports inside `make` do not affect your parent shell; this target prints
# export commands so `eval "$(make initial)"` applies them to your current terminal.
initial:
	@rm -f credentials.csv data/credentials.csv; \
	curl -fsS -X POST "$(TWISTER_ENDPOINT)/refresh" >/dev/null; \
	out=$$(AWS_ACCESS_KEY_ID=bootstrap-temp \
		AWS_SECRET_ACCESS_KEY=bootstrap-temp \
		aws iam create-access-key \
		--endpoint-url "$(TWISTER_ENDPOINT)" \
		--region "$${AWS_REGION:-us-east-1}" \
		--query 'AccessKey.[AccessKeyId,SecretAccessKey]' \
		--output text); \
	IFS=$$'\t' read -r ak sk <<< "$$out"; \
	printf 'export AWS_ACCESS_KEY_ID=%s\n' "$$ak"; \
	printf 'export AWS_SECRET_ACCESS_KEY=%s\n' "$$sk"