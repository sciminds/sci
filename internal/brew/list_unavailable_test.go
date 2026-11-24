package brew

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/samber/lo"
)

// errNoKeg mirrors what brew prints for an installed formula it has removed.
var errNoKeg = errors.New("no such keg: /opt/homebrew/Cellar/openssl@1.1")

// infoRunner answers Info like brew does: one unavailable name fails the whole
// call.
type infoRunner struct {
	mockRunner
	unavailable []string
}

func (r *infoRunner) Info(names []string, isCask bool) ([]PackageInfo, error) {
	if lo.Some(names, r.unavailable) {
		return nil, errNoKeg
	}
	pkgType := lo.Ternary(isCask, TypeCask, TypeFormula)
	return lo.Map(names, func(name string, _ int) PackageInfo {
		return PackageInfo{Name: name, Desc: "about " + name, Type: pkgType}
	}), nil
}

func TestListDetailed_UnavailablePackages(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"fd\"\nbrew \"jq\"\nbrew \"openssl@1.1\"\nbrew \"ripgrep\"\ncask \"firefox\"\ncask \"gone-app\"\n")
	r := &infoRunner{unavailable: []string{"openssl@1.1", "gone-app"}}

	got, err := ListDetailed(r, bf)
	if err != nil {
		t.Fatal(err)
	}
	want := []PackageInfo{
		{Name: "fd", Desc: "about fd", Type: TypeFormula},
		{Name: "firefox", Desc: "about firefox", Type: TypeCask},
		{Name: "gone-app", Type: TypeCask},
		{Name: "jq", Desc: "about jq", Type: TypeFormula},
		{Name: "openssl@1.1", Type: TypeFormula},
		{Name: "ripgrep", Desc: "about ripgrep", Type: TypeFormula},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ListDetailed = %+v, want %+v", got, want)
	}
}

func TestListDetailed_EveryLookupFails(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"fd\"\nbrew \"jq\"\n")
	m := &mockRunner{infoErr: errors.New("brew is broken")}

	_, err := ListDetailed(m, bf)
	if err == nil || !strings.Contains(err.Error(), "brew is broken") {
		t.Errorf("err = %v, want the brew failure", err)
	}
}
