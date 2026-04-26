# Lambda-like behavior in Twister

Twister exposes a **subset** of the AWS Lambda control and invoke API (JSON 1.1, `X-Amz-Target` `Lambda_20150331.*`) with **SigV4** signing, the same way as Secrets Manager and SSM. Functions are **Docker images** you provide; Twister runs `docker run --rm -i` per invocation.

## Requirements

- **Docker Engine** available to the Twister process: the `docker` CLI on `PATH`, or set `DOCKER_HOST` if the daemon is remote.
- The Twister user must be able to run containers (e.g. membership in the `docker` group on Linux).

## Data layout

By default, under the server `dataPath`, Twister stores:

- **Function definitions:** `{lambdaDataPath}/functions/{FunctionName}.json`
- **Event source mappings (SQS → Lambda):** `{lambdaDataPath}/event-source-mappings.json`

Override the root with **`TWISTER_LAMBDA_DATA_PATH`** (absolute path, or resolved with `dataPath` + basename from `server.json` field `lambdaDataPath`, default `lambda`).

## Container I/O contract (v1)

- **Input:** the invoke **event** is sent as UTF-8 JSON on **stdin**.
- **Output:** the function must print the **response** JSON to **stdout** (Twister base64-encodes it in the `Invoke` API response like AWS).
- **Errors:** non-zero process exit is treated as an **unhandled** function error (`FunctionError` in the invoke response).

Official **AWS Lambda base images** are built for the **Runtime API**, not stdin/stdout. For Twister you need an image whose `ENTRYPOINT` reads from stdin and writes JSON to stdout, or a thin wrapper. See the example below.

## Example: minimal image

Dockerfile:

```dockerfile
FROM alpine:3.20
# Echo stdin to stdout (valid JSON in → same JSON out for testing)
ENTRYPOINT ["cat"]
```

This copies the event bytes to the response (for quick smoke tests only).

Build and load:

```bash
docker build -t twister-echo:local .
```

## `aws` CLI examples

Use your Twister base URL and region (must match SigV4 credential scope and resources).

**Create function (container image only in v1):**

```bash
aws lambda create-function \
  --endpoint-url http://127.0.0.1:8080/ \
  --region us-east-1 \
  --function-name my-fn \
  --package-type Image \
  --code ImageUri=twister-echo:local \
  --role arn:aws:iam::000000000000:role/twister \
  --timeout 30 \
  --memory-size 256
```

**Invoke:**

```bash
aws lambda invoke \
  --endpoint-url http://127.0.0.1:8080/ \
  --region us-east-1 \
  --function-name my-fn \
  --payload '{"hello":"world"}' \
  --cli-binary-format raw-in-base64-out \
  out.json
cat out.json
```

The file `out.json` is the function’s stdout (JSON), base64-decoded by the CLI when using the flags above.

## SQS → Lambda (v1: synchronous on dequeue)

Twister can **map** an SQS queue ARN to a function. When a client performs a **normal** `ReceiveMessage` (not a peek with `VisibilityTimeout=0`), after messages are removed from the queue Twister **synchronously** invokes the mapped function with a **partial** SQS event (`Records` with `body`, `messageId`, `receiptHandle`, etc.).

This is the **“path A / demo”** behavior: it is not a long-running event-source poller like AWS, but it is enough for S3 → SQS → Lambda style flows in development.

1. Create the SQS queue and the Lambda (as above).
2. **Create event source mapping:**

```bash
aws lambda create-event-source-mapping \
  --endpoint-url http://127.0.0.1:8080/ \
  --region us-east-1 \
  --function-name my-fn \
  --event-source-arn arn:aws:sqs:us-east-1:000000000000:my-queue \
  --batch-size 1
```

Use the same queue name and region as in Twister’s SQS and your bucket notifications.

3. **S3 → SQS:** configure the bucket in Twister to send notifications to that queue (unchanged from existing Twister S3 + SQS behavior).
4. When a message is **received and dequeued** from the queue, Twister runs the container with the SQS event JSON on stdin.

## Security note

`docker run` uses `--read-only` on the root filesystem; this is **local dev**–grade hardening, not a full production isolation story.

## Automated smoke tests (repo `test/`)

With Twister running on **`127.0.0.1:8080`** and Docker available:

- **`test/test-lambda-cli.sh`** — `docker build` + `aws lambda create-function` / `invoke` / `delete-function`
- **`test/test-lambda-python.sh`** — venv, boto3, `docker build`, then **`test/lambda_invoke_boto3.py`**

The image is defined in **`test/Dockerfile.lambda-smoke`** (`ENTRYPOINT` **`cat`**, matching the stdin/stdout contract). See the root **`README.md`** **Tests** section for a short summary.
