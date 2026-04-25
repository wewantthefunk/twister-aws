#!/usr/bin/env python3
"""
Read an SSM parameter from Twister (or real AWS) via boto3 and print Parameter Value to stdout.

  pip install -r test/requirements.txt
  export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...   # must match server credentials.csv
  ./test/get_parameter_boto3.py --endpoint-url http://127.0.0.1:8080 --name /twister/demo

Environment (optional): TWISTER_ENDPOINT_URL, AWS_DEFAULT_REGION, PARAMETER_NAME — see --help.
"""

from __future__ import annotations

import argparse
import os
import sys


def main() -> int:
    p = argparse.ArgumentParser(description="Get an SSM parameter value with boto3 (stdout = Value).")
    p.add_argument(
        "--endpoint-url",
        default=os.environ.get("TWISTER_ENDPOINT_URL", "http://127.0.0.1:8080"),
        help="SSM endpoint (Twister: include scheme and port). "
        "Default: env TWISTER_ENDPOINT_URL or http://127.0.0.1:8080",
    )
    p.add_argument(
        "--region",
        default=os.environ.get("AWS_DEFAULT_REGION", "us-east-1"),
        help="Signing region (must match the parameter row). Default: AWS_DEFAULT_REGION or us-east-1",
    )
    p.add_argument(
        "--name",
        default=os.environ.get("PARAMETER_NAME", "/twister/demo"),
        help="Parameter name (path). Default: env PARAMETER_NAME or /twister/demo",
    )
    p.add_argument(
        "--with-decryption",
        action="store_true",
        help="Set WithDecryption=True (required for SecureString on real AWS; Twister enforces the same rule).",
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
        "ssm",
        endpoint_url=args.endpoint_url,
        region_name=args.region,
    )

    get_kw: dict = {"Name": args.name}
    if args.with_decryption:
        get_kw["WithDecryption"] = True

    try:
        resp = client.get_parameter(**get_kw)
    except client.exceptions.ParameterNotFound:
        print(f"error: parameter not found: {args.name!r}", file=sys.stderr)
        return 2
    except Exception as e:
        print(f"error: {e}", file=sys.stderr)
        return 3

    p = resp.get("Parameter") or {}
    if "Value" in p:
        v = p["Value"]
        sys.stdout.write(v)
        if not str(v).endswith("\n"):
            sys.stdout.write("\n")
    else:
        print("error: no Value in GetParameter response", file=sys.stderr)
        return 4
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
