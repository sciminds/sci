package lab

import (
	"strings"
	"testing"
)

func TestSetupResult_JSON(t *testing.T) {
	r := SetupResult{OK: true, User: "e3jolly", Message: "lab configured"}
	j, ok := r.JSON().(SetupResult)
	if !ok {
		t.Fatal("JSON() should return SetupResult")
	}
	if !j.OK || j.User != "e3jolly" {
		t.Errorf("unexpected JSON: %+v", j)
	}
}

func TestSetupResult_Human(t *testing.T) {
	r := SetupResult{OK: true, User: "e3jolly", Message: "lab configured"}
	h := r.Human()
	if !strings.Contains(h, "lab configured") {
		t.Errorf("Human() missing message: %q", h)
	}
}

func TestLsResult_JSON(t *testing.T) {
	r := LsResult{Path: "/labs/sciminds/data", Raw: "total 4\ndrwxr-xr-x 2 root root 4096 Jan 1 00:00 .\n"}
	j, ok := r.JSON().(LsResult)
	if !ok {
		t.Fatal("JSON() should return LsResult")
	}
	if j.Path != "/labs/sciminds/data" {
		t.Errorf("Path = %q", j.Path)
	}
}

func TestLsResult_Human(t *testing.T) {
	raw := "total 4\n-rw-r--r-- 1 user group 1234 Jan 1 00:00 results.csv\n"
	r := LsResult{Path: "/labs/sciminds/data", Raw: raw}
	h := r.Human()
	if !strings.Contains(h, "results.csv") {
		t.Errorf("Human() missing file listing: %q", h)
	}
}

func TestLsResult_Empty(t *testing.T) {
	r := LsResult{Path: "/labs/sciminds/data", Raw: ""}
	h := r.Human()
	if !strings.Contains(h, "empty") {
		t.Errorf("Human() should indicate empty: %q", h)
	}
}
