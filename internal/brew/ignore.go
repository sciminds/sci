package brew

// ignore.go — the user's list of packages sci must not upgrade.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/sciconfig"
)

// Package types as they appear in the ignore list and on [OutdatedPackage].
const (
	// TypeFormula is a Homebrew formula.
	TypeFormula = "formula"
	// TypeCask is a Homebrew cask.
	TypeCask = "cask"
	// TypeUV is a uv tool.
	TypeUV = "uv"
)

// ignorableTypes lists every type the ignore list accepts, in display order.
var ignorableTypes = []string{TypeFormula, TypeCask, TypeUV}

// ErrAmbiguousType reports a package name that matches more than one type when
// the caller did not choose one.
var ErrAmbiguousType = errors.New("pass --formula, --cask, or --uv")

// ErrNotInstalled reports an attempt to ignore a package that is not installed.
var ErrNotInstalled = errors.New("not installed")

// IgnoredPackage identifies one ignored package. Type is part of the identity
// because a formula and a cask can share a name.
type IgnoredPackage struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// IgnoreList is the set of packages sci leaves out of every upgrade.
type IgnoreList []IgnoredPackage

// ignoreConfig is the on-disk shape of tools.json.
type ignoreConfig struct {
	Ignored IgnoreList `json:"ignored"`
}

var ignoreFile = sciconfig.File[ignoreConfig]{Name: "tools.json"}

// ignoreKey returns the canonical form of a package: bare lowercase names for
// formulae and casks (see [shortFormula]), normalized names without extras for
// uv tools.
func ignoreKey(pkgType, name string) IgnoredPackage {
	if pkgType == TypeUV {
		bare, _, _ := strings.Cut(name, "[")
		return IgnoredPackage{Type: pkgType, Name: normalizePkgName(bare)}
	}
	return IgnoredPackage{Type: pkgType, Name: strings.ToLower(strings.TrimSpace(shortFormula(name)))}
}

// Contains reports whether the list ignores the named package of pkgType.
func (l IgnoreList) Contains(pkgType, name string) bool {
	key := ignoreKey(pkgType, name)
	return lo.ContainsBy(l, func(p IgnoredPackage) bool {
		return ignoreKey(p.Type, p.Name) == key
	})
}

// LoadIgnored reads the ignore list. A missing file means nothing is ignored.
// A file that cannot be fully understood, including one with misspelled keys,
// is an error: guessing could upgrade a package the user asked sci to leave
// alone.
func LoadIgnored() (IgnoreList, error) {
	data, err := ignoreFile.LoadRaw()
	if err != nil {
		return nil, fmt.Errorf("read ignore list: %w", err)
	}
	if data == nil {
		return nil, nil
	}
	list, err := parseIgnored(data)
	if err != nil {
		return nil, fmt.Errorf("read ignore list %s: %w", ignoreFile.Path(), err)
	}
	return list, nil
}

// parseIgnored decodes tools.json strictly. Plain json.Unmarshal would skip a
// misspelled key and leave its package unprotected.
func parseIgnored(data []byte) (IgnoreList, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cfg ignoreConfig
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected content after the first JSON value")
	}
	for _, p := range cfg.Ignored {
		if !slices.Contains(ignorableTypes, p.Type) {
			return nil, fmt.Errorf("unknown type %q for %q (want %s)", p.Type, p.Name, strings.Join(ignorableTypes, ", "))
		}
		if ignoreKey(p.Type, p.Name).Name == "" {
			return nil, fmt.Errorf("%s entry has no name", p.Type)
		}
	}
	return cfg.Ignored, nil
}

// Ignore adds an installed package to the ignore list. An empty pkgType is
// resolved from what is installed; a name installed as several types returns
// [ErrAmbiguousType]. dryRun reports the change without saving it.
func Ignore(r Runner, name, pkgType string, dryRun bool) (IgnoreResult, error) {
	list, err := LoadIgnored()
	if err != nil {
		return IgnoreResult{}, err
	}
	snap, err := CollectSnapshot(r)
	if err != nil {
		return IgnoreResult{}, err
	}
	installed := IgnoreList(slices.Concat(
		ignoreEntries(TypeFormula, snap.Formulae),
		ignoreEntries(TypeCask, snap.Casks),
		ignoreEntries(TypeUV, snap.UVTools),
	))

	matches := matchingTypes(installed, name, pkgType)
	if len(matches) == 0 {
		return IgnoreResult{}, fmt.Errorf("%q is %w", name, ErrNotInstalled)
	}
	if len(matches) > 1 {
		return IgnoreResult{}, ambiguousTypeError(name, matches)
	}

	key := ignoreKey(matches[0], name)
	result := IgnoreResult{Action: "ignore", Package: key, DryRun: dryRun}
	if list.Contains(key.Type, key.Name) {
		return result, nil
	}
	result.Changed = true
	if dryRun {
		return result, nil
	}
	return result, ignoreFile.Save(&ignoreConfig{Ignored: append(list, key)})
}

// Unignore removes a package from the ignore list, whether or not it is still
// installed. An empty pkgType matches any type; a name ignored as several types
// returns [ErrAmbiguousType]. dryRun reports the change without saving it.
func Unignore(name, pkgType string, dryRun bool) (IgnoreResult, error) {
	list, err := LoadIgnored()
	if err != nil {
		return IgnoreResult{}, err
	}

	matches := matchingTypes(list, name, pkgType)
	result := IgnoreResult{Action: "unignore", Package: IgnoredPackage{Type: pkgType, Name: name}, DryRun: dryRun}
	if len(matches) == 0 {
		return result, nil
	}
	if len(matches) > 1 {
		return IgnoreResult{}, ambiguousTypeError(name, matches)
	}

	key := ignoreKey(matches[0], name)
	result.Package = key
	result.Changed = true
	if dryRun {
		return result, nil
	}
	kept := lo.Reject(list, func(p IgnoredPackage, _ int) bool {
		return ignoreKey(p.Type, p.Name) == key
	})
	return result, ignoreFile.Save(&ignoreConfig{Ignored: kept})
}

func ignoreEntries(pkgType string, names []string) []IgnoredPackage {
	return lo.Map(names, func(name string, _ int) IgnoredPackage {
		return IgnoredPackage{Type: pkgType, Name: name}
	})
}

// matchingTypes returns the types under which list holds name, limited to
// pkgType when it is set.
func matchingTypes(list IgnoreList, name, pkgType string) []string {
	return lo.Filter(ignorableTypes, func(typ string, _ int) bool {
		return (pkgType == "" || pkgType == typ) && list.Contains(typ, name)
	})
}

func ambiguousTypeError(name string, types []string) error {
	return fmt.Errorf("%q matches %s: %w", name, strings.Join(types, " and "), ErrAmbiguousType)
}
