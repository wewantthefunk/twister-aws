package sigv4

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func formatAuthorization(accessKey, dateStamp, region, service, signedHeadersList, signatureHex string) string {
	return fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s/%s/%s/aws4_request, SignedHeaders=%s, Signature=%s",
		accessKey, dateStamp, region, service, signedHeadersList, signatureHex,
	)
}

func TestVerify_acceptsValidSignature(t *testing.T) {
	accessKey := "AKTEST"
	secretKey := "secret"
	region := "us-east-1"
	dateStamp := "20200102"
	amzDate := "20200102T120000Z"
	body := []byte(`{"SecretId":"my-test-secret"}`)
	host := "127.0.0.1:8080"

	payloadHash := sha256Hex(body)
	hdrs := map[string][]string{
		"content-type":         {`application/x-amz-json-1.1`},
		"host":                 {host},
		"x-amz-content-sha256": {payloadHash},
		"x-amz-date":           {amzDate},
		"x-amz-target":         {"secretsmanager.GetSecretValue"},
	}
	signedNames := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	sort.Strings(signedNames)
	signedList := strings.Join(signedNames, ";")

	cr := canonicalRequest("POST", "/", "", signedNames, hdrs, payloadHash)
	crHash := sha256Hex([]byte(cr))
	credScope := dateStamp + "/" + region + "/secretsmanager/aws4_request"
	sts := stringToSign(amzDate, credScope, crHash)
	sig := hmacSHA256(signingKey(secretKey, dateStamp, region, "secretsmanager"), sts)
	sigHex := hex.EncodeToString(sig)

	u := &url.URL{Scheme: "http", Host: host, Path: "/"}
	req := &http.Request{
		Method: "POST",
		URL:    u,
		Host:   host,
		Header: http.Header{
			"Content-Type":         []string{`application/x-amz-json-1.1`},
			"X-Amz-Content-Sha256": []string{payloadHash},
			"X-Amz-Date":           []string{amzDate},
			"X-Amz-Target":         []string{"secretsmanager.GetSecretValue"},
			"Authorization":        []string{formatAuthorization(accessKey, dateStamp, region, "secretsmanager", signedList, sigHex)},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	gotRegion, gotSvc, err := Verify(req, body, map[string]string{accessKey: secretKey}, time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gotRegion != region || gotSvc != "secretsmanager" {
		t.Fatalf("region = %q service = %q", gotRegion, gotSvc)
	}
}

func TestVerify_acceptsValidSignature_iam(t *testing.T) {
	accessKey := "AKTEST"
	secretKey := "secret"
	region := "us-east-1"
	dateStamp := "20200102"
	amzDate := "20200102T120000Z"
	body := []byte("Action=CreateAccessKey&Version=2010-05-08")
	host := "127.0.0.1:8080"

	payloadHash := sha256Hex(body)
	hdrs := map[string][]string{
		"content-type":         {"application/x-www-form-urlencoded; charset=utf-8"},
		"host":                 {host},
		"x-amz-content-sha256": {payloadHash},
		"x-amz-date":           {amzDate},
	}
	signedNames := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(signedNames)
	signedList := strings.Join(signedNames, ";")

	cr := canonicalRequest("POST", "/", "", signedNames, hdrs, payloadHash)
	crHash := sha256Hex([]byte(cr))
	credScope := dateStamp + "/" + region + "/iam/aws4_request"
	sts := stringToSign(amzDate, credScope, crHash)
	sig := hmacSHA256(signingKey(secretKey, dateStamp, region, "iam"), sts)
	sigHex := hex.EncodeToString(sig)

	u := &url.URL{Scheme: "http", Host: host, Path: "/"}
	req := &http.Request{
		Method: "POST",
		URL:    u,
		Host:   host,
		Header: http.Header{
			"Content-Type":         []string{"application/x-www-form-urlencoded; charset=utf-8"},
			"X-Amz-Content-Sha256": []string{payloadHash},
			"X-Amz-Date":           []string{amzDate},
			"Authorization":        []string{formatAuthorization(accessKey, dateStamp, region, "iam", signedList, sigHex)},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	gotRegion, gotSvc, err := Verify(req, body, map[string]string{accessKey: secretKey}, time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gotRegion != region || gotSvc != "iam" {
		t.Fatalf("region = %q service = %q", gotRegion, gotSvc)
	}
}

func TestVerify_unknownAccessKey(t *testing.T) {
	req := httptestMinimalReq(t, "missing", "secret")
	_, _, err := Verify(req, []byte(`{}`), map[string]string{"other": "x"}, time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "access key not in allowed credentials list") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerify_wrongSecret(t *testing.T) {
	accessKey := "AKTEST"
	body := []byte(`{}`)
	host := "example.local:8080"
	dateStamp := "20200102"
	amzDate := "20200102T120000Z"
	region := "us-east-1"
	payloadHash := sha256Hex(body)
	hdrs := map[string][]string{
		"content-type":         {`application/x-amz-json-1.1`},
		"host":                 {host},
		"x-amz-content-sha256": {payloadHash},
		"x-amz-date":           {amzDate},
		"x-amz-target":         {"secretsmanager.GetSecretValue"},
	}
	signedNames := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	sort.Strings(signedNames)
	signedList := strings.Join(signedNames, ";")
	cr := canonicalRequest("POST", "/", "", signedNames, hdrs, payloadHash)
	crHash := sha256Hex([]byte(cr))
	sts := stringToSign(amzDate, dateStamp+"/"+region+"/secretsmanager/aws4_request", crHash)
	sig := hmacSHA256(signingKey("correct-secret", dateStamp, region, "secretsmanager"), sts)
	sigHex := hex.EncodeToString(sig)

	u := &url.URL{Scheme: "http", Host: host, Path: "/"}
	req := &http.Request{
		Method: "POST",
		URL:    u,
		Host:   host,
		Header: http.Header{
			"Content-Type":         []string{`application/x-amz-json-1.1`},
			"X-Amz-Content-Sha256": []string{payloadHash},
			"X-Amz-Date":           []string{amzDate},
			"X-Amz-Target":         []string{"secretsmanager.GetSecretValue"},
			"Authorization":        []string{formatAuthorization(accessKey, dateStamp, region, "secretsmanager", signedList, sigHex)},
		},
	}

	_, _, err := Verify(req, body, map[string]string{accessKey: "wrong-secret"}, time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func httptestMinimalReq(t *testing.T, ak, sk string) *http.Request {
	t.Helper()
	body := []byte(`{}`)
	host := "h:9"
	amzDate := "20200102T120000Z"
	dateStamp := "20200102"
	region := "us-east-1"
	payloadHash := sha256Hex(body)
	hdrs := map[string][]string{
		"content-type":         {`application/x-amz-json-1.1`},
		"host":                 {host},
		"x-amz-content-sha256": {payloadHash},
		"x-amz-date":           {amzDate},
		"x-amz-target":         {"secretsmanager.GetSecretValue"},
	}
	signedNames := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	sort.Strings(signedNames)
	signedList := strings.Join(signedNames, ";")
	cr := canonicalRequest("POST", "/", "", signedNames, hdrs, payloadHash)
	crHash := sha256Hex([]byte(cr))
	sts := stringToSign(amzDate, dateStamp+"/"+region+"/secretsmanager/aws4_request", crHash)
	sig := hmacSHA256(signingKey(sk, dateStamp, region, "secretsmanager"), sts)
	sigHex := hex.EncodeToString(sig)
	u := &url.URL{Scheme: "http", Host: host, Path: "/"}
	return &http.Request{
		Method: "POST",
		URL:    u,
		Host:   host,
		Header: http.Header{
			"Content-Type":         []string{`application/x-amz-json-1.1`},
			"X-Amz-Content-Sha256": []string{payloadHash},
			"X-Amz-Date":           []string{amzDate},
			"X-Amz-Target":         []string{"secretsmanager.GetSecretValue"},
			"Authorization":        []string{formatAuthorization(ak, dateStamp, region, "secretsmanager", signedList, sigHex)},
		},
	}
}
