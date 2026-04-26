#!/usr/bin/env bash
# Build the smoke image, venv, boto3, then run test/lambda_invoke_boto3.py against Twister.
# Same credentials pattern as test/test-aws-python.sh (data/credentials.csv).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CRED_CSV="${ROOT}/data/credentials.csv"

if [[ ! -f "$CRED_CSV" ]]; then
  echo "error: credentials file not found: ${CRED_CSV}" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker not found (required for Lambda smoke test)" >&2
  exit 1
fi

# shellcheck disable=SC2016,SC2091
set +e
export_script="$(
  python3 - "$CRED_CSV" <<'PY'
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
)"
py_ec=$?
set -e
if [[ $py_ec -ne 0 || -z "$export_script" ]]; then
  echo "error: no valid access_key_id,secret row in ${CRED_CSV}" >&2
  exit 1
fi
eval "$export_script"

cd "$ROOT"
SMOKE_TAG="twister-lambda-smoke:local"
echo "Building ${SMOKE_TAG} from test/Dockerfile.lambda-smoke …" >&2
docker build -f test/Dockerfile.lambda-smoke -t "${SMOKE_TAG}" "${ROOT}" >/dev/null

if [[ ! -d .venv ]]; then
  python3 -m venv .venv
fi
# shellcheck source=/dev/null
. .venv/bin/activate
pip install --disable-pip-version-check -q -r "${ROOT}/test/requirements.txt"

echo "=== lambda create + invoke (boto3) ===" >&2
exec "${ROOT}/test/lambda_invoke_boto3.py" --endpoint-url http://127.0.0.1:8080 --region us-east-1
