package lab

import "testing"

func TestSafeReadPath(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		want    string
		wantErr bool
	}{
		{"simple file", "data/results.csv", "/labs/sciminds/data/results.csv", false},
		{"subdir", "data/experiment/run1", "/labs/sciminds/data/experiment/run1", false},
		{"root", "", "/labs/sciminds", false},
		{"dot", ".", "/labs/sciminds", false},
		{"dotdot rejected", "../etc/passwd", "", true},
		{"embedded dotdot", "data/../../etc/passwd", "", true},
		{"absolute rejected", "/etc/passwd", "", true},
		{"trailing slash", "data/", "/labs/sciminds/data", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeReadPath(tt.rel)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSafeWritePath(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	tests := []struct {
		name    string
		rel     string
		want    string
		wantErr bool
	}{
		{"simple file", "results.csv", "/labs/sciminds/sci/e3jolly/results.csv", false},
		{"subdir", "experiment/run1.csv", "/labs/sciminds/sci/e3jolly/experiment/run1.csv", false},
		{"root", "", "/labs/sciminds/sci/e3jolly", false},
		{"dotdot rejected", "../other_user/data.csv", "", true},
		{"absolute rejected", "/tmp/hack", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeWritePath(cfg, tt.rel)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildLsArgs(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildLsArgs(cfg, "/labs/sciminds/data")
	want := []string{"ssh", cfg.SSHAlias(), "ls", "-1lh", "'/labs/sciminds/data'"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildGetArgs(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildGetArgs(cfg, "/labs/sciminds/data/results.csv", ".")
	want := []string{"rsync", "-avz", "-s", "--progress", cfg.SSHAlias() + ":/labs/sciminds/data/results.csv", "."}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildGetArgs_Dir(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildGetArgs(cfg, "/labs/sciminds/data/experiment/", "./local/")
	want := []string{"rsync", "-avz", "-s", "--progress", cfg.SSHAlias() + ":/labs/sciminds/data/experiment/", "./local/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildPutArgs(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildPutArgs(cfg, "results.csv", "/labs/sciminds/sci/e3jolly/results.csv", false)
	want := []string{"rsync", "-avz", "-s", "--progress", "results.csv", cfg.SSHAlias() + ":/labs/sciminds/sci/e3jolly/results.csv"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildPutArgs_DryRun(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildPutArgs(cfg, "results.csv", "/labs/sciminds/sci/e3jolly/results.csv", true)
	want := []string{"rsync", "-avz", "-s", "--progress", "--dry-run", "results.csv", cfg.SSHAlias() + ":/labs/sciminds/sci/e3jolly/results.csv"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildOpenArgs(t *testing.T) {
	cfg := &Config{User: "e3jolly"}
	got := BuildOpenArgs(cfg)
	want := []string{"ssh", "-t", cfg.SSHAlias(), "cd /labs/sciminds && exec $SHELL -l"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
