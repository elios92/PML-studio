//go:build windows

package main

import (
    "strings"
    "testing"
)

func TestNormalizeRuntimeCanonicalPBSPaths(t *testing.T) {
    input := []byte(`a=root/"PBS"; b=root / "PBS" / "town_map.txt"; c=self.project_root / "PBS" / "metadata.txt"; d=project_root / "PBS"`)
    got := string(normalizeRuntimeCanonicalPBSPaths(input))
    for _, legacy := range []string{`root/"PBS"`, `root / "PBS"`, `project_root / "PBS"`} {
        if strings.Contains(got, legacy) {
            t.Fatalf("legacy root PBS path survived normalization: %s", got)
        }
    }
    for _, want := range []string{`root/"converted"/"PBS"`, `root / "converted" / "PBS"`, `self.project_root / "converted" / "PBS"`, `project_root / "converted" / "PBS"`} {
        if !strings.Contains(got, want) {
            t.Fatalf("missing canonical PBS path %q in %s", want, got)
        }
    }
}
