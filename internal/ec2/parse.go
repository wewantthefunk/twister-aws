package ec2

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func listIndexed(q url.Values, base string) []string {
	var out []string
	for i := 1; i < 100; i++ {
		v := strings.TrimSpace(q.Get(fmt.Sprintf("%s.%d", base, i)))
		if v == "" {
			break
		}
		out = append(out, v)
	}
	return out
}

type nameValuesFilter struct {
	Name   string
	Values []string
}

func parseFilters(q url.Values) []nameValuesFilter {
	var fs []nameValuesFilter
	for i := 1; i < 100; i++ {
		name := strings.TrimSpace(q.Get(fmt.Sprintf("Filters.%d.Name", i)))
		if name == "" {
			break
		}
		var vals []string
		for j := 1; j < 100; j++ {
			v := strings.TrimSpace(q.Get(fmt.Sprintf("Filters.%d.Values.%d", i, j)))
			if v == "" {
				break
			}
			vals = append(vals, v)
		}
		fs = append(fs, nameValuesFilter{Name: name, Values: vals})
	}
	return fs
}

func parseTagSpecifications(q url.Values) map[string]string {
	tags := map[string]string{}
	for n := 1; n < 50; n++ {
		if strings.TrimSpace(q.Get(fmt.Sprintf("TagSpecification.%d.ResourceType", n))) == "" {
			continue
		}
		for m := 1; m < 50; m++ {
			k := strings.TrimSpace(q.Get(fmt.Sprintf("TagSpecification.%d.Tags.%d.Key", n, m)))
			if k == "" {
				break
			}
			v := strings.TrimSpace(q.Get(fmt.Sprintf("TagSpecification.%d.Tags.%d.Value", n, m)))
			tags[k] = v
		}
	}
	return tags
}

func parseIpPermissions(q url.Values) ([]ingressRule, error) {
	var rules []ingressRule
	for n := 1; n < 50; n++ {
		prefix := fmt.Sprintf("IpPermissions.%d", n)
		proto := strings.TrimSpace(q.Get(prefix + ".IpProtocol"))
		if proto == "" {
			// no more permission blocks
			if n == 1 && len(q) > 0 {
				// try flat first permission (some clients)
				if strings.TrimSpace(q.Get("IpProtocol")) != "" {
					r, err := onePermissionFromPrefix(q, "")
					if err != nil {
						return nil, err
					}
					return []ingressRule{r}, nil
				}
			}
			break
		}
		rule := ingressRule{Protocol: proto}
		if fp := strings.TrimSpace(q.Get(prefix + ".FromPort")); fp != "" {
			x, err := strconv.Atoi(fp)
			if err != nil {
				return nil, fmt.Errorf("FromPort")
			}
			rule.FromPort = x
		}
		if tp := strings.TrimSpace(q.Get(prefix + ".ToPort")); tp != "" {
			x, err := strconv.Atoi(tp)
			if err != nil {
				return nil, fmt.Errorf("ToPort")
			}
			rule.ToPort = x
		}
		cidr := ""
		for m := 1; m < 50; m++ {
			c := strings.TrimSpace(q.Get(fmt.Sprintf("%s.IpRanges.%d.CidrIp", prefix, m)))
			if c == "" {
				break
			}
			cidr = c
		}
		if cidr == "" {
			cidr = "0.0.0.0/0"
		}
		rule.CidrIPv4 = cidr
		rules = append(rules, rule)
	}
	return rules, nil
}

func onePermissionFromPrefix(q url.Values, prefix string) (ingressRule, error) {
	p := func(suffix string) string {
		if prefix == "" {
			return strings.TrimSpace(q.Get(suffix))
		}
		return strings.TrimSpace(q.Get(prefix + "." + suffix))
	}
	proto := p("IpProtocol")
	if proto == "" {
		return ingressRule{}, fmt.Errorf("missing protocol")
	}
	rule := ingressRule{Protocol: proto}
	if fp := p("FromPort"); fp != "" {
		x, err := strconv.Atoi(fp)
		if err != nil {
			return ingressRule{}, err
		}
		rule.FromPort = x
	}
	if tp := p("ToPort"); tp != "" {
		x, err := strconv.Atoi(tp)
		if err != nil {
			return ingressRule{}, err
		}
		rule.ToPort = x
	}
	rule.CidrIPv4 = p("CidrIp")
	if rule.CidrIPv4 == "" {
		rule.CidrIPv4 = "0.0.0.0/0"
	}
	return rule, nil
}

func parseMinMaxCount(q url.Values) (minC, maxC int, err error) {
	minC, maxC = 1, 1
	if s := strings.TrimSpace(q.Get("MinCount")); s != "" {
		minC, err = strconv.Atoi(s)
		if err != nil || minC < 1 {
			return 0, 0, fmt.Errorf("MinCount")
		}
	}
	if s := strings.TrimSpace(q.Get("MaxCount")); s != "" {
		maxC, err = strconv.Atoi(s)
		if err != nil || maxC < 1 {
			return 0, 0, fmt.Errorf("MaxCount")
		}
	}
	if minC != 1 || maxC != 1 {
		return 0, 0, fmt.Errorf("only a single instance (MinCount=MaxCount=1) is supported")
	}
	return minC, maxC, nil
}

func containsFold(slice []string, s string) bool {
	for _, x := range slice {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
