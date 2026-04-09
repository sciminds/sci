package api

import "testing"

func TestParseNextLink(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "standard next link",
			header: `<https://canvas.ucsd.edu/api/v1/courses/123/users?page=2&per_page=100>; rel="next"`,
			want:   "https://canvas.ucsd.edu/api/v1/courses/123/users?page=2&per_page=100",
		},
		{
			name:   "multiple links",
			header: `<https://api.example.com?page=1>; rel="first", <https://api.example.com?page=2>; rel="next", <https://api.example.com?page=5>; rel="last"`,
			want:   "https://api.example.com?page=2",
		},
		{
			name:   "no next link",
			header: `<https://api.example.com?page=1>; rel="first", <https://api.example.com?page=5>; rel="last"`,
			want:   "",
		},
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
		{
			name:   "github style with extra spaces",
			header: `<https://api.github.com/classrooms/123/assignments?page=2&per_page=100>; rel="next", <https://api.github.com/classrooms/123/assignments?page=3&per_page=100>; rel="last"`,
			want:   "https://api.github.com/classrooms/123/assignments?page=2&per_page=100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseNextLink(tt.header)
			if got != tt.want {
				t.Errorf("ParseNextLink() = %q, want %q", got, tt.want)
			}
		})
	}
}
