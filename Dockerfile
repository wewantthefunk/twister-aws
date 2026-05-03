# Twister: production image with persistent data under /app (mount a volume there).
# Build:  docker build -t twister .   or   make build
# Run:    docker run -p 8080:8080 -v /path/to/data:/app twister
# For EC2/Lambda from *inside* this image: `make run` mounts the host socket when `DOCKER_SOCK` exists (default path below). Or `docker run … -v /var/run/docker.sock:…` manually.
# Bind mounts: host files are owned by a host user. The image defaults to USER twister (10001) for
# non-bind runs; for a project dir mount, use a matching UID/GID, e.g.  make run  (uses your id)
# or  docker run --user $(id -u):$(id -g) -v… :/app  …
# Development / AI patterns: repository file  .agents/AI_CODING_GUIDE.md  (not copied into the image)

FROM golang:1.22-alpine AS build
RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod ./
# go.sum is optional (stdlib-only); download populates the module cache
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/twister ./cmd/twister

# ---

FROM alpine:3.20

LABEL org.opencontainers.image.title="Twister" \
	org.opencontainers.image.description="Local HTTP service compatible with AWS CLI/SDK SigV4 (e.g. Secrets Manager, IAM). Dev guide: .agents/AI_CODING_GUIDE.md in the source tree." \
	org.opencontainers.image.source="https://github.com/christian/twister"

RUN apk add --no-cache ca-certificates docker-cli \
	&& addgroup -S -g 10001 twister \
	&& adduser -S -u 10001 -G twister twister

COPY --from=build /out/twister /usr/local/bin/twister
RUN chmod 0755 /usr/local/bin/twister

# Data: credentials.csv, secrets.csv, parameters.csv, twister.log, etc. (server.json basenames).
# S3 buckets live as subdirectories of s3DataPath (default "buckets") → /app/buckets when dataPath is /app.
ENV TWISTER_DATA_PATH=/app
VOLUME ["/app"]
WORKDIR /app
# When not using a bind mount, the process is twister. With --user from the host, files under /app match mount ownership.
USER twister
EXPOSE 8080

# Default listen :8080 from server.json; override with -e on docker run or mount server.json
ENTRYPOINT ["/usr/local/bin/twister"]
