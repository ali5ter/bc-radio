package main

import (
	"reflect"
	"testing"
)

// The rewrite covers the single-dash long form only: two-letter shorthands
// are what cobra's scan already handles, --flag is canonical, and
// -flag=value carries its value on the same word (which cobra drops
// whole, so it needs no rewrite).
func TestNormalizeArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"-username", "ali5ter"}, []string{"--username", "ali5ter"}},
		{[]string{"-u", "ali5ter"}, []string{"-u", "ali5ter"}},
		{[]string{"--username", "ali5ter"}, []string{"--username", "ali5ter"}},
		{[]string{"-username=ali5ter"}, []string{"-username=ali5ter"}},
		{[]string{"-j"}, []string{"-j"}},
		{[]string{"-volume", "-0.5"}, []string{"--volume", "--0.5"}},
		{[]string{"--"}, []string{"--"}},
		{[]string{}, []string{}},
	}
	for _, tc := range cases {
		got := normalizeArgs(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("normalizeArgs(%v) = %v; want %v", tc.in, got, tc.want)
		}
	}
}
