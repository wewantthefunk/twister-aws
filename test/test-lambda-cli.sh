#!/usr/bin/env bash
# Smoke test: build a tiny "echo stdin" image, register it with Twister, invoke, then delete the function.
# Requires: AWS CLI v2, Docker, Twister on 127.0.0.1:8080 with a credential in credentials.csv matching AWS_*.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v aws >/dev/null 2>&1; then
  echo "error: aws CLI not found" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker not found (required for Lambda smoke test)" >&2
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

SMOKE_TAG="twister-lambda-smoke:local"
SMOKE_FN="twister-smoke-lambda"
ENDPOINT="http://127.0.0.1:8080"
REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}"

echo "Building ${SMOKE_TAG} from test/Dockerfile.lambda-smoke …" >&2
docker build -f test/Dockerfile.lambda-smoke -t "${SMOKE_TAG}" "${ROOT}" >/dev/null

# Load credentials from data/credentials.csv if AWS_ACCESS_KEY_ID is unset
if [[ -z "${AWS_ACCESS_KEY_ID:-}" && -f "${ROOT}/data/credentials.csv" ]]; then
  # shellcheck disable=SC2016,SC2091
  export_script="$(
    python3 - "${ROOT}/data/credentials.csv" <<'PY'
import csv, shlex, sys
path = sys.argv[1]
with open(path, newline="") as f:
    rows = list(csv.reader(f))
start = 0
if len(rows) > 0 and len(rows[0]) >= 2:
    h = rows[0][0].strip().lower()
    if h in ("access_key_id", "access key id"):
        start = 1
for row in rows[start:]:
    if len(row) < 2:
        continue
    ak, sk = row[0].strip(), row[1].strip()
    if ak and sk:
        print(f"export AWS_ACCESS_KEY_ID={shlex.quote(ak)}")
        print(f"export AWS_SECRET_ACCESS_KEY={shlex.quote(sk)}")
        sys.exit(0)
sys.exit(1)
PY
  )" || true
  if [[ -n "${export_script}" ]]; then
    eval "$export_script"
  fi
fi

if [[ -z "${AWS_ACCESS_KEY_ID:-}" ]]; then
  echo "error: set AWS_ACCESS_KEY_ID (and secret) to match the server allowlist, or add data/credentials.csv" >&2
  exit 1
fi

echo "Using AWS_ACCESS_KEY_ID='${AWS_ACCESS_KEY_ID}' (lambda SigV4 scope: lambda) …" >&2

cleanup() {
  aws lambda delete-function \
    --endpoint-url "${ENDPOINT}" \
    --region "${REGION}" \
    --function-name "${SMOKE_FN}" 2>/dev/null || true
}
trap cleanup EXIT

cleanup

aws lambda create-function \
  --endpoint-url "${ENDPOINT}" \
  --region "${REGION}" \
  --function-name "${SMOKE_FN}" \
  --package-type Image \
  --code "ImageUri=${SMOKE_TAG}" \
  --role "arn:aws:iam::000000000000:role/twister-smoke" \
  --timeout 30 \
  --memory-size 256

OUT_JSON="$(mktemp)"
PAYLOAD='{"smokeBash":true}'

# CLI v2: raw JSON payload with base64 on the wire
aws lambda invoke \
  --endpoint-url "${ENDPOINT}" \
  --region "${REGION}" \
  --function-name "${SMOKE_FN}" \
  --cli-binary-format raw-in-base64-out \
  --payload "${PAYLOAD}" \
  "${OUT_JSON}"

# Decode Payload field and compare to what we sent
dec="$(
  python3 -c "
import base64, json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    d = json.load(f)
pl = d.get('Payload', '')
if d.get('FunctionError'):
    print('error payload:', pl, file=sys.stderr)
    sys.exit(1)
raw = base64.b64decode(pl)
sys.stdout.write(raw.decode('utf-8'))
" "${OUT_JSON}"
)"

rm -f "${OUT_JSON}"
trap - EXIT

if [[ "${dec}" != "${PAYLOAD}" ]]; then
  echo "error: invoke payload mismatch" >&2
  echo "  want: ${PAYLOAD}" >&2
  echo "  got:  ${dec}" >&2
  exit 1
fi

cleanup
trap - EXIT

echo "ok: lambda create + invoke (bash CLI)" >&2
