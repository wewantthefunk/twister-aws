#!/usr/bin/env bash
# From repo: test/test-aws-python.sh — venv, boto3 smoke tests against Twister
# (Secrets Manager get-secret, then SSM get-parameter).
# Picks the first data row (after optional header) from data/credentials.csv and exports
# AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (same path rules as a server using data/ for creds).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CRED_CSV="${ROOT}/data/credentials.csv"

if [[ ! -f "$CRED_CSV" ]]; then
  echo "error: credentials file not found: ${CRED_CSV}" >&2
  echo "  (expected path: <repo>/data/credentials.csv relative to this script in test/)" >&2
  exit 1
fi

# shellcheck disable=SC2016,SC2091,SC2312
set +e
export_script="$(python3 - "$CRED_CSV" <<'PY'
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
unset export_script

cd "$ROOT"
if [[ ! -d .venv ]]; then
  python3 -m venv .venv
fi
# shellcheck source=/dev/null
. .venv/bin/activate
pip install --disable-pip-version-check -q -r "${ROOT}/test/requirements.txt"

echo "=== get_secret_value (us-west-1, other-secret) ===" >&2
"${ROOT}/test/get_secret_boto3.py" --endpoint-url http://127.0.0.1:8080 --secret-id other-secret --region us-west-1

echo "=== get_parameter (us-east-1, /twister/demo) ===" >&2
"${ROOT}/test/get_parameter_boto3.py" --endpoint-url http://127.0.0.1:8080 --name /twister/demo --region us-east-1
