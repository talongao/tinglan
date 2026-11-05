package main

import (
	"strings"
	"testing"
)

func TestComputePlugins(t *testing.T) {
	available := []string{"huawei", "sample"}
	cases := []struct {
		name    string
		event   string
		ref     string
		value   string
		want    string
		wantErr string
	}{
		{name: "main push builds every plugin", event: "push", ref: "refs/heads/main", want: "all"},
		{name: "develop push is rejected", event: "push", ref: "refs/heads/develop", wantErr: "main-only"},
		{name: "manual all", event: "workflow_dispatch", ref: "refs/heads/develop", value: "all", want: "all"},
		{name: "manual none", event: "workflow_dispatch", ref: "refs/heads/develop", value: "  ", want: ""},
		{name: "manual picks one", event: "workflow_dispatch", ref: "refs/heads/develop", value: "huawei", want: "huawei"},
		{name: "manual trims and dedupes", event: "workflow_dispatch", ref: "refs/heads/develop", value: " huawei , sample ,huawei ", want: "huawei,sample"},
		{name: "unknown id", event: "workflow_dispatch", ref: "refs/heads/develop", value: "nope", wantErr: "unknown plugin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := computePlugins(tc.event, tc.ref, tc.value, available)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("plugins = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestComputeMatrixRequiresPlatform(t *testing.T) {
	none := func(string) bool { return false }
	if _, err := computeMatrix("workflow_dispatch", "refs/heads/develop", none); err == nil {
		t.Fatal("expected an error when no platform is selected")
	}
}
