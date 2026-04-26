#!/usr/bin/env python3
"""
Create a container-backed function on Twister, invoke it, verify stdin/stdout round-trip, then delete.

  pip install -r test/requirements.txt
  # optional: use data/credentials.csv via test/test-lambda-python.sh
  export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
  python3 test/lambda_invoke_boto3.py --endpoint-url http://127.0.0.1:8080

Requires Docker: the function image (default twister-lambda-smoke:local) must exist;
build with: docker build -f test/Dockerfile.lambda-smoke -t twister-lambda-smoke:local .
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Any


def main() -> int:
    p = argparse.ArgumentParser(
        description="Twister Lambda smoke: create (Image), invoke, delete (boto3).",
    )
    p.add_argument(
        "--endpoint-url",
        default=os.environ.get("TWISTER_ENDPOINT_URL", "http://127.0.0.1:8080"),
        help="Lambda endpoint (include scheme and port). Default: env TWISTER_ENDPOINT_URL or http://127.0.0.1:8080",
    )
    p.add_argument(
        "--region",
        default=os.environ.get("AWS_DEFAULT_REGION", "us-east-1"),
        help="SigV4 region (use lambda in credential scope). Default: us-east-1",
    )
    p.add_argument(
        "--function-name",
        default=os.environ.get("TWISTER_LAMBDA_SMOKE_NAME", "twister-smoke-lambda"),
        help="Function name to create/delete. Default: twister-smoke-lambda",
    )
    p.add_argument(
        "--image-uri",
        default=os.environ.get("TWISTER_LAMBDA_SMOKE_IMAGE", "twister-lambda-smoke:local"),
        help="Local Docker image tag passed to create-function. Default: twister-lambda-smoke:local",
    )
    p.add_argument(
        "--role-arn",
        default="arn:aws:iam::000000000000:role/twister-smoke",
        help="Dummy IAM role ARN (required by the API).",
    )
    p.add_argument(
        "--keep-function",
        action="store_true",
        help="Do not delete the function after a successful run (debug).",
    )
    p.add_argument(
        "--profile",
        default=os.environ.get("AWS_PROFILE"),
        help="Optional boto3 profile.",
    )
    args = p.parse_args()

    try:
        import boto3
    except ImportError:
        print("error: boto3 is required: pip install -r test/requirements.txt", file=sys.stderr)
        return 1

    if not (os.environ.get("AWS_ACCESS_KEY_ID") and os.environ.get("AWS_SECRET_ACCESS_KEY")):
        print(
            "error: set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY to match the server allowlist",
            file=sys.stderr,
        )
        return 1

    session_kw: dict[str, Any] = {}
    if args.profile:
        session_kw["profile_name"] = args.profile
    session = boto3.session.Session(**session_kw)
    client = session.client("lambda", endpoint_url=args.endpoint_url, region_name=args.region)

    fn = args.function_name
    # Must match the exact bytes the container returns on stdout (no extra spaces)
    want_payload = json.dumps({"smokePython": True}, separators=(",", ":"))

    def delete_fn() -> None:
        if args.keep_function:
            return
        try:
            client.delete_function(FunctionName=fn)
        except client.exceptions.ResourceNotFoundException:
            pass
        except Exception:
            pass

    try:
        try:
            client.delete_function(FunctionName=fn)
        except client.exceptions.ResourceNotFoundException:
            pass

        client.create_function(
            FunctionName=fn,
            PackageType="Image",
            Code={"ImageUri": args.image_uri},
            Role=args.role_arn,
            Timeout=30,
            MemorySize=256,
        )

        resp = client.invoke(
            FunctionName=fn,
            Payload=want_payload,
        )
        if resp.get("StatusCode", 0) not in (200, 201):
            print("error: unexpected StatusCode", resp, file=sys.stderr)
            delete_fn()
            return 2
        if resp.get("FunctionError"):
            pl = resp.get("Payload")
            body = pl.read() if pl is not None else b""
            print("error: FunctionError", body.decode("utf-8", errors="replace"), file=sys.stderr)
            delete_fn()
            return 3

        raw = resp["Payload"].read()
        if isinstance(raw, bytes):
            out = raw.decode("utf-8")
        else:
            out = str(raw)
        if out != want_payload:
            print("error: payload mismatch", file=sys.stderr)
            print("  want:", want_payload, file=sys.stderr)
            print("  got: ", out, file=sys.stderr)
            delete_fn()
            return 4
    except client.exceptions.ResourceConflictException as e:
        print("error: function already exists (delete it or use another name):", e, file=sys.stderr)
        delete_fn()
        return 5
    except Exception as e:
        print("error:", e, file=sys.stderr)
        delete_fn()
        return 6

    delete_fn()
    if args.keep_function:
        print("ok: lambda create + invoke (boto3); function left registered:", fn, file=sys.stderr)
    else:
        print("ok: lambda create + invoke (boto3)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
