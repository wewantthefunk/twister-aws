package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Verify checks AWS SigV4. now is used for X-Amz-Date skew checks.
// The signing service in the credential scope (e.g. secretsmanager, iam) is returned and used for the signing key.
func Verify(r *http.Request, body []byte, allowedSecrets map[string]string, now time.Time) (region, signingService string, err error) {
	clientHash := strings.TrimSpace(r.Header.Get("X-Amz-Content-Sha256"))
	var payloadHash string
	switch {
	case strings.EqualFold(clientHash, "UNSIGNED-PAYLOAD"):
		payloadHash = "UNSIGNED-PAYLOAD"
	case clientHash == "":
		payloadHash = sha256Hex(body)
	default:
		payloadHash = sha256Hex(body)
		if !strings.EqualFold(payloadHash, clientHash) {
			return "", "", errors.New("payload hash mismatch")
		}
	}

	auth := r.Header.Get("Authorization")
	credField, signedHeadersField, sigHex, err := parseAuthorization(auth)
	if err != nil {
		return "", "", err
	}

	ak, dateStamp, region, signingService, _, err := parseCredentialScope(credField)
	if err != nil {
		return "", "", err
	}
	secretAccessKey, ok := allowedSecrets[ak]
	if !ok {
		return "", "", errors.New("access key not in allowed credentials list")
	}

	amzDate := r.Header.Get("X-Amz-Date")
	if amzDate == "" {
		return "", "", errors.New("missing X-Amz-Date")
	}
	if len(amzDate) < 8 || amzDate[:8] != dateStamp {
		return "", "", errors.New("X-Amz-Date does not match credential scope date")
	}
	if err := verifyClockSkew(amzDate, now.UTC()); err != nil {
		return "", "", err
	}

	signedNames := strings.Split(signedHeadersField, ";")
	for i := range signedNames {
		signedNames[i] = strings.TrimSpace(strings.ToLower(signedNames[i]))
	}
	sort.Strings(signedNames)

	hdrs := headerMap(r)
	method := r.Method
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := canonicalQueryV4(r.URL.Query())

	cr := canonicalRequest(method, path, query, signedNames, hdrs, payloadHash)
	crHash := sha256Hex([]byte(cr))

	credentialScope := dateStamp + "/" + region + "/" + signingService + "/aws4_request"
	sts := stringToSign(amzDate, credentialScope, crHash)

	sigBytes, err := hex.DecodeString(strings.ToLower(sigHex))
	if err != nil || len(sigBytes) != sha256.Size {
		return "", "", errors.New("invalid signature encoding")
	}

	key := signingKey(secretAccessKey, dateStamp, region, signingService)
	expected := hmacSHA256(key, sts)
	if subtle.ConstantTimeCompare(sigBytes, expected) != 1 {
		return "", "", errors.New("signature mismatch")
	}

	return region, signingService, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func signingKey(secretKey, dateStamp, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	return k
}

func normalizeHeaderValue(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

func parseAuthorization(auth string) (credential, signedHeaders, signature string, err error) {
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		return "", "", "", errors.New("unsupported authorization scheme")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "))
	parts := strings.Split(rest, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "Credential="):
			credential = strings.TrimPrefix(p, "Credential=")
		case strings.HasPrefix(p, "SignedHeaders="):
			signedHeaders = strings.TrimPrefix(p, "SignedHeaders=")
		case strings.HasPrefix(p, "Signature="):
			signature = strings.TrimPrefix(p, "Signature=")
		}
	}
	if credential == "" || signedHeaders == "" || signature == "" {
		return "", "", "", errors.New("malformed authorization header")
	}
	return credential, signedHeaders, signature, nil
}

func parseCredentialScope(credential string) (accessKey, dateStamp, region, service, scopeSuffix string, err error) {
	parts := strings.Split(credential, "/")
	if len(parts) != 5 {
		return "", "", "", "", "", errors.New("invalid credential scope")
	}
	accessKey = parts[0]
	dateStamp = parts[1]
	region = parts[2]
	service = parts[3]
	scopeSuffix = parts[4]
	if len(dateStamp) != 8 || scopeSuffix != "aws4_request" {
		return "", "", "", "", "", errors.New("invalid credential scope fields")
	}
	if !isAllowedSigningService(service) {
		return "", "", "", "", "", errors.New("unsupported service in credential scope: " + service)
	}
	return accessKey, dateStamp, region, service, scopeSuffix, nil
}

func isAllowedSigningService(s string) bool {
	switch s {
	case "secretsmanager", "iam", "ssm", "s3", "sqs":
		return true
	default:
		return false
	}
}

func headerMap(r *http.Request) map[string][]string {
	m := make(map[string][]string)
	for k, vs := range r.Header {
		kl := strings.ToLower(k)
		m[kl] = append(m[kl], vs...)
	}
	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}
	if host != "" {
		if _, ok := m["host"]; !ok {
			m["host"] = []string{host}
		}
	}
	return m
}

func canonicalHeaders(signedNames []string, hdrs map[string][]string) string {
	sort.Strings(signedNames)
	var b strings.Builder
	for _, name := range signedNames {
		vals, ok := hdrs[name]
		if !ok {
			vals = []string{""}
		}
		combined := strings.Join(vals, ",")
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(normalizeHeaderValue(combined))
		b.WriteByte('\n')
	}
	return b.String()
}

func canonicalRequest(method, path, query string, signedHeaderNames []string, hdrs map[string][]string, payloadHash string) string {
	ch := canonicalHeaders(signedHeaderNames, hdrs)
	sort.Strings(signedHeaderNames)
	signed := strings.Join(signedHeaderNames, ";")
	return strings.Join([]string{
		method,
		path,
		query,
		ch,
		signed,
		payloadHash,
	}, "\n")
}

func stringToSign(amzDate, credentialScope, hashedCanonical string) string {
	return strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hashedCanonical,
	}, "\n")
}

func parseAmzDate(amzDate string) (time.Time, error) {
	return time.Parse("20060102T150405Z", amzDate)
}

func verifyClockSkew(amzDate string, now time.Time) error {
	t, err := parseAmzDate(amzDate)
	if err != nil {
		return err
	}
	skew := now.Sub(t)
	if skew > 15*time.Minute || skew < -15*time.Minute {
		return errors.New("request date outside allowed skew")
	}
	return nil
}
