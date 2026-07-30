package main

import "testing"

func TestDisplayTag(t *testing.T) {
	t.Parallel()
	if got := displayTag(""); got != "(none)" {
		t.Fatalf("empty: %q", got)
	}
	if got := displayTag("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("tag: %q", got)
	}
}

func TestNextVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current, bump, explicit, want string
	}{
		{"", "patch", "", "v0.0.1"},
		{"v0.1.0", "patch", "", "v0.1.1"},
		{"v0.1.0", "minor", "", "v0.2.0"},
		{"v0.1.0", "major", "", "v1.0.0"},
		{"v1.2.3", "patch", "", "v1.2.4"},
		{"v0.1.0", "patch", "v9.8.7", "v9.8.7"},
		{"v0.1.0", "patch", "1.0.0", "v1.0.0"},
	}
	for _, tc := range cases {
		got, err := nextVersion(tc.current, tc.bump, tc.explicit)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		if got != tc.want {
			t.Fatalf("%+v: got %s want %s", tc, got, tc.want)
		}
	}
}
