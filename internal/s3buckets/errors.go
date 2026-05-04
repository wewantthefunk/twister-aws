package s3buckets

import (
	"errors"
	"syscall"
)

// Sentinel errors (compare with errors.Is).
var (
	ErrInvalidBucketName   = errors.New("s3: invalid bucket name")
	ErrInvalidObjectKey    = errors.New("s3: invalid object key")
	ErrInvalidRegion       = errors.New("s3: invalid region")
	ErrBucketAlreadyExists = errors.New("s3: bucket already exists")
	ErrNoSuchBucket        = errors.New("s3: no such bucket")
	ErrBucketNotEmpty      = errors.New("s3: bucket is not empty")
	ErrNoSuchKey           = errors.New("s3: no such key")
	ErrInvalidListContinuation = errors.New("s3: invalid list continuation token")
)

func isDirNotEmptyErr(err error) bool {
	return err != nil && errors.Is(err, syscall.ENOTEMPTY)
}
