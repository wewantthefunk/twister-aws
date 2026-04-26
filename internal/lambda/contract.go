// Package lambda implements a minimal AWS Lambda–compatible surface for Twister: function
// registry on disk, Docker-based invocation, and optional SQS → Lambda triggers.
//
// # Twister container contract (v1)
//
// Images are run with `docker run --rm -i` (and resource limits). The invocation **event**
// JSON is written to the container's **stdin** (UTF-8). The container **must** write the
// response JSON to **stdout**. Non-zero exit codes are treated as function errors.
//
// Official AWS Lambda base images (e.g. public.ecr.aws/lambda/nodejs:20) expect the
// Lambda Runtime API and are not wired by default. For Twister v1, use images whose
// ENTRYPOINT/CMD read an event from stdin and print a JSON result to stdout, or a small
// wrapper script. See docs/LAMBDA.md for an example.
//
// Environment passed to the container includes: AWS_REQUEST_ID, AWS_LAMBDA_FUNCTION_NAME,
// AWS_LAMBDA_FUNCTION_MEMORY_SIZE, AWS_DEFAULT_REGION (from signing region).
package lambda
