package main

import (
	"reflect"
	"testing"
)

func TestParseAccessGroups(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "/llm-users", []string{"/llm-users"}},
		{"trims and drops empties", " /a , /b ,,", []string{"/a", "/b"}},
		{"only separators and spaces", " , ", nil},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAccessGroups(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseAccessGroups(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInAccessGroup(t *testing.T) {
	tests := []struct {
		name   string
		groups []string
		access []string
		want   bool
	}{
		{"member", []string{"/foobar", "/llm-users"}, []string{"/llm-users"}, true},
		{"non-member", []string{"/foobar"}, []string{"/llm-users"}, false},
		{"nil groups (missing claim)", nil, []string{"/llm-users"}, false},
		{"empty groups", []string{}, []string{"/llm-users"}, false},
		{"parent-only", []string{"/eng"}, []string{"/eng/llm-users"}, false},
		{"child-only", []string{"/eng/llm-users"}, []string{"/eng"}, false},
		{"bare name", []string{"llm-users"}, []string{"/llm-users"}, false},
		{"case differs", []string{"/LLM-users"}, []string{"/llm-users"}, false},
		{"second access group matches", []string{"/b"}, []string{"/a", "/b"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inAccessGroup(tt.groups, tt.access); got != tt.want {
				t.Errorf("inAccessGroup(%q, %q) = %v, want %v", tt.groups, tt.access, got, tt.want)
			}
		})
	}
}
