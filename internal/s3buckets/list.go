package s3buckets

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ListedObject is one row for ListObjectsV2 Contents.
type ListedObject struct {
	Key          string
	LastModified time.Time
	Size         int64
	ETag         string
}

// ListObjectsV2 returns object metadata for all keys in the bucket with the given
// prefix, sorted by key, using continuation-token as a 0-based offset into that
// sorted list (same encoding as the NextContinuationToken we return). maxKeys
// is clamped to [1, 1000].
func (m *Manager) ListObjectsV2(region, bucket, prefix string, maxKeys int, continuationToken string) ([]ListedObject, bool, string, error) {
	if !IsValidRegionSegment(NormalizeRegion(region)) {
		return nil, false, "", ErrInvalidRegion
	}
	if !IsValidBucketName(bucket) {
		return nil, false, "", ErrInvalidBucketName
	}
	if m == nil || m.Root == "" {
		return nil, false, "", errors.New("s3: empty root")
	}
	bp := m.BucketPath(region, bucket)
	if _, err := os.Stat(bp); err != nil {
		if os.IsNotExist(err) {
			return nil, false, "", ErrNoSuchBucket
		}
		return nil, false, "", err
	}
	if maxKeys < 1 {
		maxKeys = 1000
	}
	if maxKeys > 1000 {
		maxKeys = 1000
	}

	keys, err := collectObjectKeysUnderPrefix(bp, prefix)
	if err != nil {
		return nil, false, "", err
	}
	sort.Strings(keys)

	start := 0
	if continuationToken != "" {
		n, err := strconv.Atoi(continuationToken)
		if err != nil || n < 0 {
			return nil, false, "", ErrInvalidListContinuation
		}
		if n > len(keys) {
			n = len(keys)
		}
		start = n
	}
	end := start + maxKeys
	if end > len(keys) {
		end = len(keys)
	}
	pageKeys := keys[start:end]
	isTruncated := end < len(keys)
	var next string
	if isTruncated {
		next = strconv.Itoa(end)
	}

	objs := make([]ListedObject, 0, len(pageKeys))
	for _, key := range pageKeys {
		p, err := m.objectPath(region, bucket, key)
		if err != nil {
			return nil, false, "", err
		}
		fi, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, false, "", err
		}
		if fi.IsDir() {
			continue
		}
		etag, err := fileMD5Hex(p)
		if err != nil {
			return nil, false, "", err
		}
		objs = append(objs, ListedObject{
			Key:          key,
			LastModified: fi.ModTime().UTC(),
			Size:         fi.Size(),
			ETag:         fmt.Sprintf("%q", etag),
		})
	}
	return objs, isTruncated, next, nil
}

func collectObjectKeysUnderPrefix(bucketRoot, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(bucketRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(bucketRoot, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func fileMD5Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.CopyBuffer(h, f, make([]byte, 32*1024)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
