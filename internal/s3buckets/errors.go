package s3buckets

import (
	"errors"
	"syscall"
)

// Sentinel errors (compare with errors.Is).
var (
	ErrInvalidBucketName   = errors.New("s3: invalid bucket name")
	ErrBucketAlreadyExists = errors.New("s3: bucket already exists")
	ErrNoSuchBucket        = errors.New("s3: no such bucket")
	ErrBucketNotEmpty      = errors.New("s3: bucket is not empty")
)

func isDirNotEmptyErr(err error) bool {
	return err != nil && errors.Is(err, syscall.ENOTEMPTY)
}
