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

func TestAccessGroupsOf(t *testing.T) {
	tests := []struct {
		name   string
		groups []string
		access []string
		want   []string
	}{
		{"member", []string{"/foobar", "/llm-users"}, []string{"/llm-users"}, []string{"/llm-users"}},
		{"non-member", []string{"/foobar"}, []string{"/llm-users"}, nil},
		{"nil groups (missing claim)", nil, []string{"/llm-users"}, nil},
		{"empty groups", []string{}, []string{"/llm-users"}, nil},
		{"parent-only", []string{"/eng"}, []string{"/eng/llm-users"}, nil},
		{"child-only", []string{"/eng/llm-users"}, []string{"/eng"}, nil},
		{"bare name", []string{"llm-users"}, []string{"/llm-users"}, nil},
		{"case differs", []string{"/LLM-users"}, []string{"/llm-users"}, nil},
		{"second access group matches", []string{"/b"}, []string{"/a", "/b"}, []string{"/b"}},
		{"several matches keep claim order", []string{"/b", "/x", "/a"}, []string{"/a", "/b"}, []string{"/b", "/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := accessGroupsOf(tt.groups, tt.access); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("accessGroupsOf(%q, %q) = %q, want %q", tt.groups, tt.access, got, tt.want)
			}
		})
	}
}
