package lab

import (
	"reflect"
	"testing"
)

func TestBuildSizeArgs(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildSizeArgs(cfg, []string{"/labs/sciminds/data/a", "/labs/sciminds/data/b"})
	want := []string{"ssh", "scilab-alice", "du", "-sbc", "'/labs/sciminds/data/a'", "'/labs/sciminds/data/b'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildSizeArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestBuildSizeArgs_PathWithSpace(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildSizeArgs(cfg, []string{"/labs/x/my data"})
	want := []string{"ssh", "scilab-alice", "du", "-sbc", "'/labs/x/my data'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildSizeArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestParseDuTotal(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{
			name: "single",
			in:   "1234\t/labs/sciminds/data/a\n1234\ttotal\n",
			want: 1234,
		},
		{
			name: "multiple aggregated",
			in: "1024\t/a\n" +
				"2048\t/b\n" +
				"3072\ttotal\n",
			want: 3072,
		},
		{
			name:    "no total line",
			in:      "1024\t/a\n",
			wantErr: true,
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDuTotal(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ParseDuTotal = %d, want %d", got, tc.want)
			}
		})
	}
}
