package awsserver

import "context"

type ctxKey int

const (
	ctxKeyRegion ctxKey = iota
)

// WithSigningRegion returns a context with the region taken from the SigV4 credential scope.
func WithSigningRegion(ctx context.Context, region string) context.Context {
	return context.WithValue(ctx, ctxKeyRegion, region)
}

// RegionFromContext returns the signing region, or "us-east-1" if unset.
func RegionFromContext(ctx context.Context) string {
	v := ctx.Value(ctxKeyRegion)
	if s, ok := v.(string); ok {
		return s
	}
	return "us-east-1"
}
