package brew

import (
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// DetectedPackage represents a package found by one of the detection probes.
type DetectedPackage struct {
	Name string // package name as queried
	Type string // "formula", "cask", or "uv"
}

// Label returns a display string like "htop (formula)".
func (d DetectedPackage) Label() string {
	return d.Name + " (" + d.Type + ")"
}

// Prober abstracts the three detection probes for testability.
type Prober interface {
	ProbeFormula(pkg string) (bool, error)
	ProbeCask(pkg string) (bool, error)
	ProbePyPI(pkg string) (bool, error)
}

// Detect probes all backends concurrently and returns matches in priority
// order: cask > formula > uv. Probe errors are treated as "not found".
func Detect(p Prober, pkg string) ([]DetectedPackage, error) {
	type result struct {
		found bool
		typ   string
		order int
	}

	var (
		results [3]result
		wg      sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		found, _ := p.ProbeCask(pkg)
		results[0] = result{found: found, typ: "cask", order: 0}
	}()
	go func() {
		defer wg.Done()
		found, _ := p.ProbeFormula(pkg)
		results[1] = result{found: found, typ: "formula", order: 1}
	}()
	go func() {
		defer wg.Done()
		found, _ := p.ProbePyPI(pkg)
		results[2] = result{found: found, typ: "uv", order: 2}
	}()
	wg.Wait()

	var matches []DetectedPackage
	for _, r := range results {
		if r.found {
			matches = append(matches, DetectedPackage{Name: pkg, Type: r.typ})
		}
	}
	return matches, nil
}

// LiveProber implements Prober by shelling out to brew and hitting PyPI.
type LiveProber struct{}

func (LiveProber) ProbeFormula(pkg string) (bool, error) {
	err := exec.Command("brew", "info", "--json=v2", pkg).Run()
	if err != nil {
		return false, err
	}
	return true, nil
}

func (LiveProber) ProbeCask(pkg string) (bool, error) {
	err := exec.Command("brew", "info", "--json=v2", "--cask", pkg).Run()
	if err != nil {
		return false, err
	}
	return true, nil
}

func (LiveProber) ProbePyPI(pkg string) (bool, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(fmt.Sprintf("https://pypi.org/pypi/%s/json", pkg))
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}
