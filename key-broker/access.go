package main

import (
	"slices"
	"strings"
)

func parseAccessGroups(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func inAccessGroup(groups, access []string) bool {
	return slices.ContainsFunc(groups, func(g string) bool { return slices.Contains(access, g) })
}
