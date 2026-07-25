package integrations

import (
	"reflect"
	"testing"
)

func TestProfileSlugsFromMarkdown(t *testing.T) {
	tests := []struct {
		name   string
		bodies []string
		want   []string
	}{
		{
			name:   "bare profile url",
			bodies: []string{"hey https://linear.app/acme/profiles/ada what do you think?"},
			want:   []string{"ada"},
		},
		{
			name:   "markdown link",
			bodies: []string{"see [Ada](https://linear.app/acme/profiles/ada-lovelace) please"},
			want:   []string{"ada-lovelace"},
		},
		{
			name: "dedupe across bodies",
			bodies: []string{
				"https://linear.app/acme/profiles/ada",
				"also https://linear.app/acme/profiles/ada and https://linear.app/acme/profiles/bob",
			},
			want: []string{"ada", "bob"},
		},
		{
			name:   "ignores issue and label urls",
			bodies: []string{"https://linear.app/acme/issue/NOT-1/x and https://linear.app/acme/team/T/issue-label/bug"},
			want:   nil,
		},
		{
			name:   "empty",
			bodies: []string{"", "no mentions here"},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileSlugsFromMarkdown(tt.bodies...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("profileSlugsFromMarkdown() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
