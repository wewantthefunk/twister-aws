#!/usr/bin/env python3
"""
Read a secret from Twister (or real AWS Secrets Manager) via boto3 and print SecretString to stdout.

  pip install -r test/requirements.txt
  export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...   # must match server credentials.csv
  ./test/get_secret_boto3.py --endpoint-url http://127.0.0.1:8080 --secret-id my-test-secret

Environment (optional): TWISTER_ENDPOINT_URL, AWS_DEFAULT_REGION, SECRET_ID — see --help.
"""

from __future__ import annotations

import argparse
import os
import sys


def main() -> int:
    p = argparse.ArgumentParser(description="Get a secret value with boto3 (stdout = SecretString).")
    p.add_argument(
        "--endpoint-url",
        default=os.environ.get("TWISTER_ENDPOINT_URL", "http://127.0.0.1:8080"),
        help="Secrets Manager endpoint (Twister: include scheme and port). "
        "Default: env TWISTER_ENDPOINT_URL or http://127.0.0.1:8080",
    )
    p.add_argument(
        "--region",
        default=os.environ.get("AWS_DEFAULT_REGION", "us-east-1"),
        help="Signing region (must match the client). Default: AWS_DEFAULT_REGION or us-east-1",
    )
    p.add_argument(
        "--secret-id",
        default=os.environ.get("SECRET_ID", "my-test-secret"),
        help="Secret name or ARN-style id. Default: env SECRET_ID or my-test-secret",
    )
    p.add_argument(
        "--profile",
        default=os.environ.get("AWS_PROFILE"),
        help="Optional shared-credentials profile (boto3 session).",
    )
    args = p.parse_args()

    try:
        import boto3
    except ImportError:
        print("error: boto3 is required: pip install -r test/requirements.txt", file=sys.stderr)
        return 1

    session_kw: dict = {}
    if args.profile:
        session_kw["profile_name"] = args.profile

    session = boto3.session.Session(**session_kw)
    client = session.client(
        "secretsmanager",
        endpoint_url=args.endpoint_url,
        region_name=args.region,
    )

    try:
        resp = client.get_secret_value(SecretId=args.secret_id)
    except client.exceptions.ResourceNotFoundException:
        print(f"error: secret not found: {args.secret_id!r}", file=sys.stderr)
        return 2
    except Exception as e:
        print(f"error: {e}", file=sys.stderr)
        return 3

    if "SecretString" in resp:
        sys.stdout.write(resp["SecretString"])
        if not resp["SecretString"].endswith("\n"):
            sys.stdout.write("\n")
    else:
        print("error: SecretBinary not supported by this script", file=sys.stderr)
        return 4
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
