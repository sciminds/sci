package lab

import (
	"strings"
	"testing"
)

func TestUpgradeControlPersist_RewritesStaleValue(t *testing.T) {
	in := `Host other
    ControlPersist 1h

Host sciminds
    Hostname ssrde.ucsd.edu
    ControlPersist 4h
    User alice
`
	out, changed := upgradeControlPersist(in, "sciminds", "12h")
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(out, "    ControlPersist 12h\n") {
		t.Errorf("expected sciminds block to have ControlPersist 12h; got:\n%s", out)
	}
	if !strings.Contains(out, "Host other\n    ControlPersist 1h\n") {
		t.Errorf("other host's ControlPersist should be untouched; got:\n%s", out)
	}
}

func TestUpgradeControlPersist_NoChangeWhenAlreadyTarget(t *testing.T) {
	in := `Host sciminds
    ControlPersist 12h
`
	out, changed := upgradeControlPersist(in, "sciminds", "12h")
	if changed {
		t.Error("expected changed=false")
	}
	if out != in {
		t.Error("output should equal input when no change needed")
	}
}

func TestUpgradeControlPersist_NoBlockNoChange(t *testing.T) {
	in := `Host other
    ControlPersist 4h
`
	_, changed := upgradeControlPersist(in, "sciminds", "12h")
	if changed {
		t.Error("expected changed=false when alias not present")
	}
}
