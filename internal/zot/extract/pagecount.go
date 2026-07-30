package extract

import (
	"fmt"
	"os"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// pageBytesFallback is the bytes-per-page divisor used when a PDF can't
// be parsed. The estimate's job is ordering, not accuracy, and the two
// error directions are wildly asymmetric: under-estimating a book costs
// 20–55 minutes of head-of-line blocking, over-estimating a paper costs
// one extra ~30s docling model load. So the divisor is biased hard
// toward over-estimation: at 100 KiB/page a 6.6 MB scanned monograph
// (the observed 178-page/53-minute case) estimates 66 pages and is
// correctly isolated even when the parser fails on it.
const pageBytesFallback = 100 << 10

// pdfcpuConf returns the shared pdfcpu configuration with the config
// dir disabled. pdfcpu materialises $XDG_CONFIG_HOME/pdfcpu/ on first
// default-configuration use — sci must never write into a user's
// config tree as a side effect of counting pages, so the dir is
// disabled once and the configuration reused for every call.
var pdfcpuConf = sync.OnceValue(func() *model.Configuration {
	api.DisableConfigDir()
	return model.NewDefaultConfiguration()
})

// PageCount returns the number of pages in the PDF at path. Unlike
// EstimatePages it reports failures; use it when the caller needs to
// distinguish "unparseable" from a real count.
func PageCount(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("pagecount %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	n, err := api.PageCount(f, pdfcpuConf())
	if err != nil {
		return 0, fmt.Errorf("pagecount %s: %w", path, err)
	}
	return n, nil
}

// EstimatePages returns a never-failing per-document scheduling cost in
// pages. A parse failure falls back to a size-derived pseudo count
// (fallbackPages); a missing file estimates 1 — docling will fail it in
// seconds, so it must not eat an isolated chunk slot.
func EstimatePages(path string) int {
	if n, err := PageCount(path); err == nil && n > 0 {
		return n
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 1
	}
	return fallbackPages(fi.Size())
}

// fallbackPages converts a file size into a pseudo page count. Pure;
// floor of 1.
func fallbackPages(size int64) int {
	return max(1, int(size/pageBytesFallback))
}
