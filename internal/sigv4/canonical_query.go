package sigv4

import (
	"net/url"
	"sort"
	"strings"
)

// canonicalQueryV4 is the AWS SigV4 canonical query string for URL query parameters.
// See: https://docs.aws.amazon.com/general/latest/gr/sigv4-create-canonical-request.html
// Parameter names and values are URI-encoded; pairs are sorted by name, then by value;
// the result uses "&" with no leading separator.
func canonicalQueryV4(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	pairs := make([]struct{ k, v string }, 0)
	for name, values := range v {
		for _, val := range values {
			pairs = append(pairs, struct{ k, v string }{k: name, v: val})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(uriEncodeAWSSigV4(p.k))
		b.WriteByte('=')
		b.WriteString(uriEncodeAWSSigV4(p.v))
	}
	return b.String()
}

// uriEncodeAWSSigV4 is the "URI encode" from the SigV4 spec: percent-encode every
// byte except unreserved: A–Z, a–z, 0–9, hyphen, underscore, period, tilde.
// (Space must be %20, not "+".)
func uriEncodeAWSSigV4(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
