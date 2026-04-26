# Twister

**Twister** is a single HTTP service that aims to be a **drop-in replacement for AWS APIs** at the wire level: point the AWS SDK or CLI at Twister with `--endpoint-url` and the same `X-Amz-Target` / JSON 1.1 / SigV4 contracts (and **S3**–style **REST** for buckets). Implemented surfaces include **Secrets Manager**, **SSM Parameter Store**, path-style **S3 bucket** create/delete, **SQS** (query / JSON to `POST /`), a **Lambda**-compatible **control and invoke** API (Docker-backed; see **`docs/LAMBDA.md`**) and optional **S3 → SQS** event notifications. Each S3 bucket is a **folder** under a configurable root, e.g. `buckets/` inside your mounted data volume in Docker.

## Layout

Run commands from the **repository root** so relative paths resolve.

| Path | Purpose |
|------|---------|
| `AGENTS.md`, `CLAUDE.md`, `.cursor/skills/familiarize` | **AI agent onboarding** — read `.agents/familiarize/SKILL.md` first; Cursor exposes the same skill under `.cursor/skills/` |
| `.agents/AI_CODING_GUIDE.md` | **Contributor & AI assistant guide** — project purpose, patterns, tests, when to update this README |
| `docs/LAMBDA.md` | **Lambda** feature: Docker requirement, `lambdaDataPath` / **`TWISTER_LAMBDA_DATA_PATH`**, `aws lambda` CLI, SQS → Lambda (dequeue trigger) |
| `cmd/twister/` | `main` — compose config, credentials, `awsserver` routes, and listen |
| `internal/awsserver/` | **SigV4**; `PrimaryHandler` routes **S3** `GET`/`HEAD`/`PUT`/`DELETE` on `/bucket/...`, then `POST /` to JSON **1.1** or IAM |
| `internal/s3buckets/` | **S3** bucket operations as directories under **`s3DataPath`** (default **`buckets`**) |
| `internal/sqs/` | **SQS** queue files under **`sqsDataPath`** (default **`sqs`**) and optional post-dequeue hook to **`internal/lambda`** |
| `internal/lambda/` | **Lambda** JSON 1.1 API subset (`lambda` / **`Lambda_20150331`**) — registry on disk, **Docker** `run` per invoke |
| `internal/iam/` | **IAM** Query API subset (`CreateAccessKey` → XML), persists to **`credentials.csv`** |
| `internal/credentials/` | CSV allowlist + **`Provider`** (`VerifyRequest` / SigV4, `AddAccessKeyAndPersist`) |
| `internal/sigv4/` | SigV4 crypto (used by `credentials`) |
| `internal/secretstore/` | In-memory **secret values**; loads **`secrets.csv`** (and optional `secrets.json`) at startup |
| `internal/secretsmanager/` | **AWS Secrets Manager** API operations |
| `internal/paramstore/` | In-memory **SSM parameters**; loads **`parameters.csv`** (and optional `parameters.json`) at startup |
| `internal/ssm/` | **AWS SSM** Parameter Store–style API (`GetParameter`, `PutParameter`) |
| `test/test-aws-cli.sh` | Smoke test (waits for Twister; AWS CLI **Secrets Manager** + **SSM**) |
| `test/test-lambda-cli.sh` | Lambda smoke: **`docker build`** ( **`test/Dockerfile.lambda-smoke`** ), `aws lambda create-function` + `invoke` + delete |
| `test/get_secret_boto3.py` | **Python + boto3** example: `get-secret-value` to stdout |
| `test/get_parameter_boto3.py` | **Python + boto3** example: SSM `get-parameter` value to stdout |
| `test/lambda_invoke_boto3.py` | **Python + boto3**: create container function, **invoke** (stdin/stdout round-trip), delete |
| `test/test-aws-python.sh` | Loads **`data/credentials.csv`**, venv, runs **`get_secret_boto3.py`** and **`get_parameter_boto3.py`** |
| `test/test-lambda-python.sh` | Same venv/credentials pattern; **`docker build`** then **`lambda_invoke_boto3.py`** |
| `test/Dockerfile.lambda-smoke` | Minimal **`cat`** image used by the Lambda smoke tests (event on **stdin** → **stdout**) |
| `server.json` | Listen address, optional **`dataPath`**, per-filename entries, and optional **`mapPath`** (host dir for `make run` → `/app` in Docker; ignored at runtime) |
| `data/` | Example data directory (default **`mapPath`** in root `server.json`); `data/server.json` is used inside the container when that mount is in use |
| `credentials.csv`, **`secrets.csv`**, `secrets.json`, **`parameters.csv`**, `parameters.json` | Data files; default next to the process unless **`dataPath`** in `server.json` moves them to one directory |
| **`buckets/`** (see **`s3DataPath`**) | On-disk **S3** layout: **`buckets/{region}/{bucketName}/`** — the signing **region** scopes each bucket directory |

**Flow:** SigV4 is verified for every protected call. Path-style **`s3`** requests (`GET`/`HEAD`/`PUT`/`DELETE` under `/{bucket}/...`) go to **`internal/s3buckets`** (see `PrimaryHandler`). All other **JSON 1.1** traffic uses `POST /` and `awsserver.Router`. The **signing service** in the credential scope selects the product:

- **`iam`** (used by the AWS CLI for `aws iam …`) — **form-encoded** body (`Action=…`), **not** `X-Amz-Target`. Today **`CreateAccessKey`** is supported; the new key pair is appended and **`credentials.csv`** is rewritten.
- **`secretsmanager`** — **`application/x-amz-json-1.1`**, **`X-Amz-Target`** (e.g. `secretsmanager.GetSecretValue` → `GetSecretValue`) → `internal/secretsmanager`.
- **`ssm`** — same JSON 1.1 + **`X-Amz-Target`**. The AWS CLI sends targets like **`AmazonSSM.GetParameter`** while the SigV4 scope uses **`ssm`**; Twister treats those as the same product. Handling lives in `internal/ssm`.
- **`s3`** — **REST** (not `X-Amz-Target`): e.g. **`PUT /bucket-name`** to create, **`DELETE /bucket-name`** to remove an empty bucket, with SigV4 scope **`s3`**, matching **`aws s3 mb` / `aws s3 rb`**.
- **`sqs`** — SQS is **not** the same `POST` + `X-Amz-Target` path as JSON 1.1 secrets/ssm: the router branches on credential scope **`sqs`** to **`internal/sqs`** (form or JSON 1.0, depending on the client).
- **`lambda`** — **JSON 1.1** + `X-Amz-Target` prefix **`Lambda_20150331`** (SigV4 scope must be **`lambda`**), implemented in **`internal/lambda`**. See **`docs/LAMBDA.md`**.

Add another JSON 1.1 product by implementing `Service` and passing it to `awsserver.NewRouter` in `main`. Add S3–style behavior in `s3buckets` and route it from `awsserver.PrimaryHandler`. IAM stays on the form/query path in `internal/iam`.

### Admin HTTP routes (not AWS)

- **`GET` / **`HEAD` `/health`** — liveness; **`200`** with body `ok` (plain text).
- **`GET` / **`POST` `/refresh`** — reread **`credentials.csv`**, **`secrets.csv`**, **`secrets.json`**, **`parameters.csv`**, and **`parameters.json`** from the same resolved paths as process startup (including `TWISTER_*` overrides), clear in-memory state, reload, then apply **seed defaults** for demo secret names that have no row in any region (same order as startup). Response is JSON: `ok`, `accessKeys`, `secrets`, `parameters`, and the paths used. **Not authenticated** — only use on trusted networks or protect with a reverse proxy / firewall.

## Run

```bash
go run ./cmd/twister
```

On startup, Twister reads **`server.json`** (see below). If that file is missing, the same built-in defaults apply.

**Environment (Twister; legacy `SECRETS_LOCAL_*` still accepted):**

- **`TWISTER_SERVER_CONFIG`** (or `SECRETS_LOCAL_SERVER_CONFIG`) — path to `server.json`
- **`TWISTER_DATA_PATH`** (or `SECRETS_LOCAL_DATA_PATH`) — overrides `dataPath` from `server.json` (canonical directory for all data files)
- **`TWISTER_CREDENTIALS_CSV`** (or `SECRETS_LOCAL_CREDENTIALS_CSV`) — full path to the credential allowlist (overrides `dataPath` + `credentialsFile` if set)
- **`TWISTER_SECRETS_CSV`** (or `SECRETS_LOCAL_SECRETS_CSV`) — full path to the secret payloads CSV (overrides `dataPath` + `secretsCSV` if set)
- **`TWISTER_PARAMETERS_CSV`** — full path to the SSM parameters CSV (overrides `dataPath` + `parametersCSV` if set)
- **`TWISTER_PARAMETERS_JSON`** — full path to the optional SSM parameters JSON overlay (overrides `dataPath` + `parametersFile` if set)
- **`TWISTER_S3_DATA_PATH`** — absolute path to the directory that will contain **one subdirectory per S3 bucket** (overrides `dataPath` + `s3DataPath` when set). In Docker with the default layout, this is often `/app/buckets` (i.e. the host’s data dir + `buckets` when `dataPath` is `/app` and `s3DataPath` is `buckets`).
- **`TWISTER_SQS_DATA_PATH`** — absolute path to the directory for **SQS** queue files (overrides `dataPath` + `sqsDataPath` when set; default basename **`sqs`**).
- **`TWISTER_LAMBDA_DATA_PATH`** — absolute path to the **Lambda** registry and event–source mapping files (overrides `dataPath` + `lambdaDataPath` when set; default basename **`lambda`**). See **`docs/LAMBDA.md`**.
- **`TWISTER_PID_FILE`** (or `SECRETS_LOCAL_PID_FILE`) — full path for the PID file (overrides `dataPath` + `pidFile` if set)

**`dataPath`:** when non-empty, Twister places **`secrets.csv`**, **`parameters.csv`**, **`credentials.csv`**, **`secrets.json`**, **`parameters.json`**, and **`twister.log`** (basename only) under that directory, e.g. `dataPath: "/usr"` → `/usr/secrets.csv`, `/usr/credentials.csv`, etc. The `secretsCSV`, `parametersCSV`, `credentialsFile`, and other file keys still supply the **file name**; any directory in those strings is ignored when `dataPath` is set. When `dataPath` is empty, paths are relative to the current working directory (as before). Per-file env variables above, when set, are absolute paths and **do not** combine with `dataPath`. The **S3 bucket parent directory** is resolved the same way as other basenames: **`s3DataPath`** (default `buckets`) under `dataPath`, so with `dataPath: "/app"` you get `/app/buckets` for bucket folders. **SQS** and **Lambda** on-disk data use the same pattern: **`sqsDataPath`** (default **`sqs`**) and **`lambdaDataPath`** (default **`lambda`**) as directory names under `dataPath` unless overridden by **`TWISTER_SQS_DATA_PATH`** / **`TWISTER_LAMBDA_DATA_PATH`**.

Example `server.json`:

```json
{
  "dataPath": "",
  "mapPath": "data",
  "listenAddress": ":8080",
  "secretsCSV": "secrets.csv",
  "secretsFile": "secrets.json",
  "parametersCSV": "parameters.csv",
  "parametersFile": "parameters.json",
  "credentialsFile": "credentials.csv",
  "pidFile": "twister.log",
  "s3DataPath": "buckets",
  "sqsDataPath": "sqs",
  "lambdaDataPath": "lambda"
}
```

**`mapPath`:** set this to a **host** path (absolute or relative to the directory you run `make` from) for the data directory that `make run` bind-mounts to **`/app`** in the container. The Twister process does not read this field; `cmd/mappath` and the Makefile use it for Docker. If empty, `make run` will fail (run `go run ./cmd/mappath -config server.json` to see the error). The mounted directory should contain a **`server.json`** (e.g. `data/server.json`) and your CSV/JSON data files. For a local **`go run ./cmd/twister`** from the repo root, the process still loads the **`server.json`** in the current working directory (unless overridden by `TWISTER_SERVER_CONFIG`).

Example with a system data root:

```json
{
  "dataPath": "/var/lib/twister",
  "listenAddress": ":8080",
  "secretsCSV": "secrets.csv",
  "secretsFile": "secrets.json",
  "parametersCSV": "parameters.csv",
  "parametersFile": "parameters.json",
  "credentialsFile": "credentials.csv",
  "pidFile": "twister.log",
  "s3DataPath": "buckets",
  "sqsDataPath": "sqs",
  "lambdaDataPath": "lambda"
}
```

Listens on **`listenAddress`** (default **`:8080`**). Startup prints **`Twister pid …`** to stdout and logs **`Twister listening on …`**.

### Credentials (server)

The server does **not** read caller credentials from its own environment. It loads an allowlist from **`credentials.csv`** in the working directory (override with **`TWISTER_CREDENTIALS_CSV`** or legacy `SECRETS_LOCAL_CREDENTIALS_CSV`). If the file is **missing** or has **no data rows** (header-only is fine), the server still starts with an **empty** allowlist.

CSV format (header row optional):

```text
access_key_id,secret_access_key
AKIAEXAMPLE,secret1
another-id,secret2
```

**How this fits together on the wire**

| Side | Where credentials live | What happens |
|------|------------------------|--------------|
| **Client** (AWS CLI, boto3, SDK, etc.) | Whatever you configure for real AWS: env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`), shared credentials file, `--profile`, instance role, etc. | The client signs the HTTP request with SigV4. Only the **access key id** appears in the `Authorization` header (`Credential=<ACCESS_KEY>/...`). The **secret** stays on the client and is used to compute the signature. |
| **Server** | **`credentials.csv`** only | The server reads the access key from `Credential=...`, looks up that access key in the CSV, takes the **secret access key** from the same row, and recomputes the signature. If the pair is missing or the signature does not match, the request is rejected. |

The client’s access key id and secret **must be exactly one row** in `credentials.csv`. They do **not** need to be in the server’s environment. Example: the sample **`credentials.csv`** includes `test` / `test`; if you `export AWS_ACCESS_KEY_ID=test` and `AWS_SECRET_ACCESS_KEY=test` for the CLI, that call will verify. To use another identity, add a row to `credentials.csv` and configure the client with the same id and secret.

**First access key (empty allowlist):** with **no** keys on the server, SigV4 cannot be checked (the server has no shared secret to verify the signature). Twister allows **one** unauthenticated call: **`Action=CreateAccessKey`** on the IAM form API so you can run **`aws iam create-access-key`** to create the first key; the new pair is written to **`credentials.csv`** and loaded in memory, like creating the first row in an empty `secrets.csv`. **Treat a zero-key server as an open enrollment step**—anyone who can reach the port can obtain that first key; after that, all calls (including further `create-access-key`) require a valid key in the allowlist. Use a listen address and firewall you trust, or pre-seed a row in `credentials.csv` if you need SigV4 from the first request.

**Additional access keys (with at least one key already on the server):** the CLI signs IAM calls with service **`iam`** in the credential scope. After a successful **`aws iam create-access-key`**, Twister appends the new id and secret to **`credentials.csv`** (under the same resolved path as at startup, including **`dataPath`** + basename when configured).

```bash
# After the first key exists, use client credentials that match a row in credentials.csv:
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

aws iam create-access-key \
  --endpoint-url http://localhost:8080 \
  --region us-east-1
```

**Shortcut:** from the repo root, **`eval "$(make initial)"`** runs **`aws iam create-access-key`** against Twister (default **`http://localhost:8080`**; override **`TWISTER_ENDPOINT`**, **`PORT`**, or **`AWS_REGION`** in the Makefile / environment) and prints **`export AWS_ACCESS_KEY_ID=…`** and **`export AWS_SECRET_ACCESS_KEY=…`**, which **`eval`** applies to your current shell so the next CLI calls use the new pair.

**First run with no `credentials.csv`:** run the same `aws iam create-access-key` with **`--endpoint-url`**; the local CLI profile can be any long-term key pair (they are not checked until after the first key is stored). The response is XML (`text/xml`) like the [CreateAccessKey](https://docs.aws.amazon.com/IAM/latest/APIReference/API_CreateAccessKey.html) API. New keys work immediately for subsequent SigV4 calls because the in-memory allowlist is updated and the CSV on disk is replaced atomically.

### Secret values (Secrets Manager data plane, durable)

Secret **payloads** are **not** ephemeral: on each process start, Twister reads them from disk into memory (with optional in-memory fallbacks below).

1. **`secrets.csv`** (path from **`secretsCSV`** in `server.json`, or **`TWISTER_SECRETS_CSV`**) — primary store for as many secrets as you need. Committed to version control for demos; in production, mount or sync this file (or point the env var at a path you control).
2. **`secrets.json`** (optional) — if present, loaded **after** the CSV; an entry with the same **`name`** and **`region`** (default `us-east-1` if omitted) **overrides** the CSV row for that (region, name) pair.
3. **Seed defaults** — only if a well-known demo name has **no** row in **any** region, a built-in sample is added in `us-east-1` (so defining e.g. **`other-secret`** only in `us-west-1` does not also create a spurious `us-east-1` copy).

**`secrets.csv`** (header row optional; RFC 4180 quoting for values with commas):

```text
name,region,secretString,createdDate,versionId
my-app-db,us-west-2,"super-secret",2020-01-02T03:04:05Z,
```

**Columns:** `name`, `secretString` (or `secret_string`); optional `region` (empty → `us-east-1`); optional `createdDate` / `created_date` (RFC3339 or Unix); optional `versionId` / `version_id` (default derived if empty). **GetSecretValue** only returns a secret when the request’s SigV4 signing **region** matches the row’s `region` (same name in two regions is two independent secrets). Without a header, columns are: name, secretString, optional createdDate, optional versionId, optional region.

`secrets.json` format (array of objects), optional overlay:

```json
[
  {
    "name": "my-test-secret",
    "region": "us-east-1",
    "secretString": "{\"key\":\"value\"}",
    "createdDate": "2020-01-02T03:04:05Z",
    "versionId": "optional-version-id-string"
  }
]

```

`region` in JSON is optional (default `us-east-1`).

`createdDate` in JSON may be an RFC3339 string or a JSON number (Unix seconds, including fractional).

## Test with AWS CLI (Secrets Manager)

Use a **matching** `--region` (it must match the region used when the request is signed, usually your CLI default or `AWS_REGION`).

From the repo root, **`./test/test-aws-cli.sh`** starts **`go run ./cmd/twister`** (which loads **`credentials.csv`**), waits for `:8080`, then runs **`aws secretsmanager get-secret-value`** with client credentials that match the sample CSV (`test` / `test`).

Manual equivalent — these exports are the **client** credentials the CLI uses to sign; they must match a row in **`credentials.csv`** on the **server** host:

```bash
# Client: picked up by AWS CLI to sign the request (must match server allowlist)
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

aws secretsmanager get-secret-value \
  --secret-id my-test-secret \
  --endpoint-url http://localhost:8080 \
  --region us-east-1
```

You can use a profile instead of env vars, as long as the profile’s key pair exists in **`credentials.csv`**:

```bash
aws secretsmanager get-secret-value \
  --secret-id my-test-secret \
  --endpoint-url http://localhost:8080 \
  --region us-east-1 \
  --profile my-local-profile
```

Successful responses use `Content-Type: application/x-amz-json-1.1` and include `Name`, `SecretString`, `CreatedDate`, `ARN`, `VersionId`, and `VersionStages`, similar to the [GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html) API.

### Create or update a secret (CLI)

`create-secret` is supported with **`--secret-string`**. The same `Name` (upsert) updates the in-memory store and rewrites the **`secrets.csv`** file on disk. If the name is new, a row is added. **SecretBinary** is not supported by Twister yet.

```bash
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

aws secretsmanager create-secret \
  --name my-new-secret \
  --secret-string 'plain-or-json-string' \
  --endpoint-url http://localhost:8080 \
  --region us-east-1
```

### Parameters (SSM Parameter Store, durable)

Parameter **values** are loaded on startup like secrets: Twister reads **`parameters.csv`** first, then optional **`parameters.json`** (which **overrides** the same `(region, name)` keys as in the secrets flow). There is **no** seed step for parameters.

**`parameters.csv`** (header optional):

```text
name,region,value,type,version,lastModified
/twister/demo,us-east-1,from-parameters-csv,String,1,2020-01-15T12:00:00Z
```

**Columns:** `name` (full parameter name, often a path such as `/app/config`); `value`; optional `region` (empty → `us-east-1`); optional `type` (`String`, `StringList`, or `SecureString`, default `String`); optional `version` (integer, default `1`); optional `lastModified` (RFC3339). **GetParameter** only returns a value when the SigV4 signing **region** matches the row. Without a header, the default column order is: name, value, optional region, optional type, optional version, optional lastModified.

`parameters.json` (optional array of objects) may include `name`, `region`, `value`, `type`, `version`, and `lastModified` (RFC3339 string or Unix seconds as a number).

**GetParameter** accepts a **Name** or an **`arn:aws:ssm:…:parameter/…`** ARN; the ARN’s region must match the request signing region. For **`SecureString`**, the request must set **`WithDecryption`** to **`true`** or Twister returns an error (same rule as AWS).

**PutParameter** writes through to **`parameters.csv`** (full file replace, like `CreateSecret`). **`Overwrite`** defaults to false: creating a name that already exists in that region returns **`ParameterAlreadyExists`**. Updates set a new **Version** and **LastModifiedDate**.

### Test with AWS CLI (Parameter Store)

Use **`--region`** consistent with the parameter row (and with the path in the ARN if you pass an ARN). The CLI signs with credential scope **`ssm`**.

```bash
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

aws ssm get-parameter \
  --name /twister/demo \
  --endpoint-url http://localhost:8080 \
  --region us-east-1

aws ssm put-parameter \
  --name /my/local/param \
  --value "hello" \
  --type String \
  --endpoint-url http://localhost:8080 \
  --region us-east-1
```

To update an existing name, add **`--overwrite`**.

### S3 (buckets on disk)

Twister implements **path-style** bucket **create** and **delete** so the **AWS CLI** works with the same endpoint:

```bash
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

# Creates: <s3 root>/<region>/my-demo-buck/   (SigV4 region, e.g. us-east-1; s3 root defaults to ./buckets)
aws s3 mb s3://my-demo-buck \
  --endpoint-url http://127.0.0.1:8080 \
  --region us-east-1

aws s3 rb s3://my-demo-buck \
  --endpoint-url http://127.0.0.1:8080 \
  --region us-east-1
```

`rb` only succeeds if the bucket directory is **empty** (real S3 behavior). Bucket names must follow the **3–63** character **DNS–style** rules (lowercase letters, numbers, dot, hyphen).

**Objects** (path-style, same region scoping as buckets):

```bash
aws s3 cp ./go.mod s3://first-bucket/go.mod --endpoint-url http://127.0.0.1:8080 --region us-east-1
aws s3 cp s3://first-bucket/go.mod - --endpoint-url http://127.0.0.1:8080 --region us-east-1
```

Object keys may use `/` (stored as subfolders under `buckets/<region>/<bucket>/`). The request body is limited to **`MaxBodyBytes` (1 MiB)** in the server for `PutObject` (enough for small files and tests).

In **Docker**, mount your data directory to **`/app`**: the default bucket root is **`/app/buckets`** (or override with **`TWISTER_S3_DATA_PATH`**).

### Lambda (Docker, `aws lambda`, SQS trigger)

Twister can **create**, **invoke**, and **list** function definitions that point at **container images**; each invoke runs `docker run` with the event JSON on **stdin** and reads the result from **stdout** (not full AWS parity — see the doc).

- **Full instructions:** **`docs/LAMBDA.md`** (docker CLI, `TWISTER_LAMBDA_DATA_PATH`, `create-function` / `invoke`, **event source mapping** for SQS, and the v1 “invoke after dequeue” behavior for **S3 → SQS → Lambda** style flows).
- **Credential scope** for the CLI: **`lambda`**; **`X-Amz-Target`** uses the **`Lambda_20150331.***` prefix, like the real AWS API.

## Docker (Makefile)

- **`make build`** — build the `twister` image.
- **`make run`** — start a **detached** container; the host path comes from the **`mapPath`** value in `server.json` (override which file to read with **`SERVER_JSON=path`**, e.g. `make run SERVER_JSON=server.json`). That path is mounted at **`/app`**; `TWISTER_DATA_PATH` in the image is `/app`. Use an absolute `mapPath` or a value relative to your shell’s current directory when you invoke `make` (e.g. **`data`** for `./data` when building from the repo root). Put **`server.json`**, `credentials.csv`, and other data files in that directory as needed (the repo’s **`data/server.json`** is a starting point).
- **`make stop`** — stop running container(s) from this image.
- **`make initial`** — call **`aws iam create-access-key`** on Twister and print **`export`** lines for the new key; use **`eval "$(make initial)"`** to set **`AWS_ACCESS_KEY_ID`** and **`AWS_SECRET_ACCESS_KEY`** in your shell (requires AWS CLI and a running Twister; see IAM section above for empty-allowlist behavior).
- On SELinux (e.g. Fedora), the run recipe adds the volume **`:z`** flag so the mount is readable. Process `--user` matches your host `uid:gid` for file permissions; see the Makefile for details.

## Tests

```bash
make test
```

Same as **`go test ./...`**.

**Lambda (Docker) smoke tests** (with Twister already listening on **`:8080`**, and **`data/credentials.csv`** available for the Python wrapper):

- **`test/test-lambda-cli.sh`** — uses the **AWS CLI** (`aws lambda`); needs **Docker** to build **`twister-lambda-smoke:local`**. If **`AWS_ACCESS_KEY_ID`** is unset, the script can load the first data row from **`data/credentials.csv`** (same idea as **`test/test-lambda-python.sh`**).
- **`test/test-lambda-python.sh`** — creates/activates **`.venv`**, **`pip install -r test/requirements.txt`**, builds the smoke image, then runs **`test/lambda_invoke_boto3.py`**.

## Notes

- **Secrets Manager:** **`secretsmanager.GetSecretValue`** and **`secretsmanager.CreateSecret`** (with `SecretString`; upsert by name) are implemented. Other `X-Amz-Target` values for that service return error-shaped JSON. Unregistered **service** names in `X-Amz-Target` are rejected.
- **Parameter Store (SSM):** **`ssm.GetParameter`** and **`ssm.PutParameter`** are implemented for `String` / `StringList` / `SecureString` (with the `WithDecryption` rule above). Other SSM operations are not implemented yet.
- **S3:** path-style **`PUT/GET/HEAD/DELETE`**: **CreateBucket** / **DeleteBucket** on `/{bucket}`; **PutObject** / **GetObject** / **DeleteObject** on `/{bucket}/{key}` (arbitrary `key` depth under **`s3DataPath`/`{region}`/`{bucket}`**). `GET`/`DELETE` a bucket name alone (list / empty delete semantics) is not fully modeled.
- **SQS:** per-region queue JSON files; clients use credential scope **`sqs`**. S3 can enqueue **S3 event** payloads to a queue; see the repo’s SQS implementation for supported operations. Optional **SQS → Lambda** mapping: **`docs/LAMBDA.md`**.
- **Lambda:** subset of the **`Lambda_20150331`** control/invoke API; **Docker** required on the host. See **`docs/LAMBDA.md`** and **`internal/lambda`**.
- **IAM:** **`CreateAccessKey`** on the Query API (form body). The credential scope must be **`iam`**, not `secretsmanager` or `ssm`.
- SigV4 checks use the per-service name in the credential scope (e.g. **`secretsmanager`**, **`ssm`**, **`iam`**, **`lambda`**, plus **`s3`**, **`sqs`** for the respective wire paths), optional **±15 minute** clock skew on `X-Amz-Date`, and support for **`UNSIGNED-PAYLOAD`**, **empty** `X-Amz-Content-Sha256` (hashed body), or a **hex** content SHA256, matching common AWS CLI / SDK behavior. For **JSON 1.1** services, the scope’s service name must **match** the normalized `X-Amz-Target` prefix.

### Troubleshooting: `UnrecognizedClientException` / “security token … is invalid”

That message comes from **real AWS**, not from Twister. The CLI is talking to the cloud because **`--endpoint-url` is missing** or the shell is not using the keys in **`credentials.csv`**.

1. **Always** point the CLI at Twister, e.g. `--endpoint-url http://127.0.0.1:8080` (same port as in `server.json` / `listenAddress`).
2. **Export** client keys that match a row in **`credentials.csv`** (e.g. `test` / `test`), or use a profile that has those keys.
3. If you use **temporary** AWS credentials elsewhere, `AWS_SESSION_TOKEN` may still be set. For local Twister with long-term test keys, run **`unset AWS_SESSION_TOKEN`** (and `AWS_SECURITY_TOKEN` if set) so the request is only signed with access key + secret, matching your CSV.
