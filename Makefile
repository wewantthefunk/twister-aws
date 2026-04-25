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

.PHONY: build run stop test
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
