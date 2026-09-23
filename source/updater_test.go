//go:build windows

package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		remote, local string
		want          bool
	}{
		{"v0.6", "0.5", true},
		{"0.5.1", "0.5", true},
		{"0.5", "0.5.0", false},
		{"0.4.9", "0.5", false},
		{"v1.0.0", "0.9.9", true},
		{"bad", "0.5", false},
	}
	for _, tc := range cases {
		if got := versionNewer(tc.remote, tc.local); got != tc.want {
			t.Fatalf("versionNewer(%q, %q) = %v, want %v", tc.remote, tc.local, got, tc.want)
		}
	}
}
