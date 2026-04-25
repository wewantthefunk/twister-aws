#!/usr/bin/env bash
# Smoke test: server loads credentials.csv; AWS CLI must use a key pair that exists there.
# The CLI signs with whatever credentials you export (or your profile); the server only
# sees the access key in Authorization and verifies the signature using the CSV secret.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v aws >/dev/null 2>&1; then
  echo "error: aws CLI not found (install AWS CLI v2 and ensure it is on PATH)" >&2
  exit 1
fi

# Wait until :8080 accepts connections
ready=0
for _ in $(seq 1 100); do
  if bash -c "echo >/dev/tcp/127.0.0.1/8080" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.05
done
if [[ "${ready}" -ne 1 ]]; then
  echo "error: server did not become ready on 127.0.0.1:8080" >&2
  exit 1
fi

# set -u: use :- default so an unset key does not trip nounset; export AWS_* first if you rely on them.
echo "AWS CLI signing: AWS_ACCESS_KEY_ID='${AWS_ACCESS_KEY_ID:-}' (export to match a row in ${ROOT}/credentials.csv)" >&2

aws secretsmanager get-secret-value \
  --secret-id other-secret \
  --endpoint-url http://localhost:8080 \
  --region us-west-1

# SigV4 scope must be ssm (not secretsmanager) for this call.
aws ssm get-parameter \
  --name /twister/demo \
  --endpoint-url http://localhost:8080 \
  --region us-east-1

