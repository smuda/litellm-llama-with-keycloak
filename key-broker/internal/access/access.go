package access

import (
	"slices"
	"strings"
)

func ParseGroups(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func GroupsOf(groups, access []string) []string {
	var out []string
	for _, g := range groups {
		if slices.Contains(access, g) {
			out = append(out, g)
		}
	}
	return out
}
