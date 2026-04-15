package lab

import (
	"reflect"
	"testing"
)

func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Progress
		ok   bool
	}{
		{
			name: "typical progress2 line",
			in:   "      1,234,567  42%   12.34MB/s    0:00:05",
			want: Progress{Bytes: 1234567, Percent: 42, Rate: "12.34MB/s", ETA: "0:00:05"},
			ok:   true,
		},
		{
			name: "100 percent done",
			in:   "  9,999,999 100%  100.00MB/s    0:00:00 (xfr#3, to-chk=0/3)",
			want: Progress{Bytes: 9999999, Percent: 100, Rate: "100.00MB/s", ETA: "0:00:00"},
			ok:   true,
		},
		{
			name: "zero pct early",
			in:   "         0   0%    0.00kB/s    0:00:00",
			want: Progress{Bytes: 0, Percent: 0, Rate: "0.00kB/s", ETA: "0:00:00"},
			ok:   true,
		},
		{
			name: "non-progress line ignored",
			in:   "sending incremental file list",
			ok:   false,
		},
		{
			name: "filename line ignored",
			in:   "data/results.csv",
			ok:   false,
		},
		{
			name: "empty",
			in:   "",
			ok:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseProgressLine(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseProgressLine\n  got:  %#v\n  want: %#v", got, tc.want)
			}
		})
	}
}

func TestBuildResumableGetArgs(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildResumableGetArgs(cfg, "/labs/sciminds/data/x", "./")
	want := []string{
		"rsync", "-az", "-s",
		"--partial",
		"--append-verify",
		"--info=progress2",
		"scilab-alice:/labs/sciminds/data/x",
		"./",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildResumableGetArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestBuildResumableGetArgs_PathWithSpace(t *testing.T) {
	cfg := &Config{User: "alice"}
	// rsync -s sends paths through the protocol, so spaces in the remote
	// path don't need explicit quoting at the argv layer.
	got := BuildResumableGetArgs(cfg, "/labs/x/my data/run-01.nii.gz", "./")
	want := []string{
		"rsync", "-az", "-s",
		"--partial",
		"--append-verify",
		"--info=progress2",
		"scilab-alice:/labs/x/my data/run-01.nii.gz",
		"./",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildResumableGetArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}
