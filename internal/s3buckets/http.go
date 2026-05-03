package s3buckets

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// restError is the common S3 REST error body.
type restError struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// WriteRESTError writes an S3-style XML error (application/xml).
func WriteRESTError(w http.ResponseWriter, code int, awsCode, message string) {
	if message == "" {
		message = awsCode
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(code)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(restError{Code: awsCode, Message: message, RequestID: newRequestID()})
}

// WriteAccessDenied is used when SigV4 fails or key is not allowlisted.
func WriteAccessDenied(w http.ResponseWriter, message string) {
	WriteRESTError(w, http.StatusForbidden, "AccessDenied", message)
}

// HandleS3REST handles S3 path-style requests after authentication.
// putBody is the request body for PUT (verified by caller); it may be non-nil only for MethodPut.
func (m *Manager) HandleS3REST(w http.ResponseWriter, r *http.Request, region string, putBody []byte) {
	if m == nil {
		http.Error(w, "s3 not configured", http.StatusInternalServerError)
		return
	}
	rid := newRequestID()
	w.Header().Set("x-amz-request-id", rid)

	bucket, key, err := parseS3Path(r.URL.Path)
	if err != nil {
		WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}

	switch r.Method {
	case http.MethodPut:
		if key == "" {
			if isNotificationSubrequest(r) {
				m.putBucketNotification(w, r, region, bucket, putBody)
				return
			}
			err := m.CreateBucket(region, bucket)
			switch {
			case err == nil:
				w.WriteHeader(http.StatusOK)
			case errors.Is(err, ErrInvalidRegion):
				WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", "The region is not valid.")
			case errors.Is(err, ErrInvalidBucketName):
				WriteRESTError(w, http.StatusBadRequest, "InvalidBucketName", "The specified bucket is not valid.")
			case errors.Is(err, ErrBucketAlreadyExists):
				WriteRESTError(w, http.StatusConflict, "BucketAlreadyOwnedByYou", "Your previous request to create the named bucket succeeded and you already own it.")
			default:
				WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
			}
			return
		}
		err := m.PutObject(region, bucket, key, putBody)
		switch {
		case err == nil:
			sum := md5.Sum(putBody)
			etagHex := hex.EncodeToString(sum[:])
			w.Header().Set("ETag", fmt.Sprintf("%q", etagHex))
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, ErrInvalidRegion), errors.Is(err, ErrInvalidBucketName), errors.Is(err, ErrInvalidObjectKey):
			WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		case errors.Is(err, ErrNoSuchBucket):
			WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		default:
			WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return

	case http.MethodDelete:
		if key == "" {
			err := m.DeleteBucket(region, bucket)
			switch {
			case err == nil:
				w.WriteHeader(http.StatusNoContent)
			case errors.Is(err, ErrInvalidRegion):
				WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", "The region is not valid.")
			case errors.Is(err, ErrInvalidBucketName):
				WriteRESTError(w, http.StatusBadRequest, "InvalidBucketName", "The specified bucket is not valid.")
			case errors.Is(err, ErrNoSuchBucket):
				WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
			case errors.Is(err, ErrBucketNotEmpty):
				WriteRESTError(w, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty.")
			default:
				WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
			}
			return
		}
		err := m.DeleteObject(region, bucket, key)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrNoSuchKey):
			WriteRESTError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		case errors.Is(err, ErrInvalidRegion), errors.Is(err, ErrInvalidBucketName), errors.Is(err, ErrInvalidObjectKey):
			WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		case errors.Is(err, ErrNoSuchBucket):
			WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		default:
			WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return

	case http.MethodGet, http.MethodHead:
		if key == "" {
			if isNotificationSubrequest(r) {
				if r.Method == http.MethodGet {
					m.getBucketNotification(w, region, bucket)
					return
				}
				WriteRESTError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "A HEAD request was specified, but this cannot be used on this resource.")
				return
			}
			q := r.URL.Query()
			if r.Method == http.MethodGet && q.Get("list-type") == "2" {
				prefix := q.Get("prefix")
				maxKeys := 1000
				if s := q.Get("max-keys"); s != "" {
					if n, err := strconv.Atoi(s); err == nil && n > 0 {
						maxKeys = n
					}
				}
				if maxKeys > 1000 {
					maxKeys = 1000
				}
				cont := q.Get("continuation-token")
				objs, isTrunc, nextTok, err := m.ListObjectsV2(region, bucket, prefix, maxKeys, cont)
				if err != nil {
					switch {
					case errors.Is(err, ErrNoSuchBucket):
						WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
					case errors.Is(err, ErrInvalidRegion), errors.Is(err, ErrInvalidBucketName):
						WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
					case errors.Is(err, ErrInvalidListContinuation):
						WriteRESTError(w, http.StatusBadRequest, "InvalidArgument", "The continuation token provided is not valid")
					default:
						WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
					}
					return
				}
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				_, _ = w.Write([]byte(xml.Header))
				enc := xml.NewEncoder(w)
				_ = enc.Encode(listBucketV2Result{
					XMLName:     xml.Name{Local: "ListBucketResult"},
					Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
					Name:        bucket,
					Prefix:      prefix,
					KeyCount:    len(objs),
					MaxKeys:     maxKeys,
					IsTruncated: isTrunc,
					NextToken:   nextTok,
					Contents:    toListContentsXML(objs),
				})
				return
			}
			WriteRESTError(w, http.StatusNotImplemented, "NotImplemented", "A header you provided implies functionality that is not implemented.")
			return
		}
		opath, _, err := m.GetObjectFile(region, bucket, key)
		if err != nil {
			switch {
			case errors.Is(err, ErrNoSuchKey):
				WriteRESTError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			case errors.Is(err, ErrNoSuchBucket):
				WriteRESTError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
			case errors.Is(err, ErrInvalidRegion), errors.Is(err, ErrInvalidBucketName), errors.Is(err, ErrInvalidObjectKey):
				WriteRESTError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
			default:
				WriteRESTError(w, http.StatusInternalServerError, "InternalError", err.Error())
			}
			return
		}
		if sum, serr := md5File(opath); serr == nil {
			w.Header().Set("ETag", fmt.Sprintf("%q", hex.EncodeToString(sum)))
		}
		http.ServeFile(w, r, opath)
		return

	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type listBucketV2Result struct {
	XMLName     xml.Name         `xml:"ListBucketResult"`
	Xmlns       string           `xml:"xmlns,attr"`
	Name        string           `xml:"Name"`
	Prefix      string           `xml:"Prefix"`
	KeyCount    int              `xml:"KeyCount"`
	MaxKeys     int              `xml:"MaxKeys"`
	IsTruncated bool             `xml:"IsTruncated"`
	NextToken   string           `xml:"NextContinuationToken,omitempty"`
	Contents    []listContentXML `xml:"Contents,omitempty"`
}

type listContentXML struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

func toListContentsXML(objs []ListedObject) []listContentXML {
	out := make([]listContentXML, 0, len(objs))
	for _, o := range objs {
		out = append(out, listContentXML{
			Key:          o.Key,
			LastModified: o.LastModified.UTC().Format("2006-01-02T15:04:05.000Z"),
			ETag:         o.ETag,
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}
	return out
}

func md5File(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(b)
	return sum[:], nil
}

// parseS3Path splits path-style URL /bucket or /bucket/object/key into bucket and object key.
// A single segment is bucket-only (key == ""). More segments: first is bucket, rest joined with "/".
func parseS3Path(p string) (bucket, key string, err error) {
	if p == "" {
		return "", "", fmt.Errorf("empty path")
	}
	if p[0] != '/' {
		return "", "", fmt.Errorf("path must be absolute")
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return "", "", fmt.Errorf("missing bucket name")
	}
	parts := strings.Split(p, "/")
	if len(parts) < 1 {
		return "", "", fmt.Errorf("missing bucket name")
	}
	bucket = parts[0]
	if bucket == "" || bucket == "." || bucket == ".." {
		return "", "", fmt.Errorf("invalid bucket name")
	}
	if len(parts) == 1 {
		return bucket, "", nil
	}
	key = strings.Join(parts[1:], "/")
	if err := validateObjectKey(key); err != nil {
		return "", "", err
	}
	return bucket, key, nil
}
