package store

import "testing"

func TestLevelNorm(t *testing.T) {
	cases := []struct {
		in   Level
		want Level
	}{
		{"", LevelWorkspace}, // empty defaults to workspace
		{LevelWorkspace, LevelWorkspace},
		{LevelUser, LevelUser},
	}
	for _, tc := range cases {
		if got := tc.in.Norm(); got != tc.want {
			t.Errorf("Level(%q).Norm() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
