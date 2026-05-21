package app

// actions.go — browser.Action factories for the cloud bucket: delete,
// copy URL, download (file → fetch, folder → sync).
//
// The original listtui rolled its own delegate UpdateFunc + pending-delete
// pointer. Here each action is a self-contained value: AppliesTo gates
// file-only operations, Allowed surfaces ownership/availability rules as
// toasts, Confirm gets the two-press dance for free, and Run composes
// progress + completion + refresh into a tea.Cmd.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// Per-action timeouts.
const (
	deleteTimeout       = 30 * time.Second
	folderDeleteTimeout = 5 * time.Minute  // recursive hf rm of a whole prefix
	downloadTimeout     = 10 * time.Minute // single-file
	folderTimeout       = 30 * time.Minute // whole-prefix sync
)

// BuildActions returns the standard delete/copy-URL/download action
// trio. Each action closes over the provider so successful mutations
// (delete) can prune the in-memory listing before the browser refreshes.
func BuildActions(p *Provider) []browser.Action {
	return []browser.Action{
		deleteAction(p),
		copyURLAction(),
		downloadAction(p.Client()),
	}
}

// deleteAction surfaces the owner-only delete for files AND folders.
// Allowed rejects foreign owners with a toast (and, as a side effect,
// the empty-owner case at the bucket-root user folder); Confirm opens
// a huh modal so the destructive action requires an explicit Yes.
// ConfirmPrompt spells out the consequence — files vs recursive folder
// — so the modal can't be dismissed via muscle-memory Enter.
// Run branches on IsDir: files use `hf buckets rm`, folders use
// `hf buckets rm -R`.
func deleteAction(p *Provider) browser.Action {
	return browser.Action{
		Key: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete (own files/folders)")),
		Allowed: func(e browser.Entry) (bool, string) {
			ce, ok := e.(Entry)
			if !ok {
				return false, "unknown entry type"
			}
			owner := ce.T.Owner()
			if owner == "" {
				return false, "cannot delete the bucket-root user folder"
			}
			if owner != p.Client().Username {
				return false, fmt.Sprintf("cannot delete @%s's files — only the owner can", owner)
			}
			return true, ""
		},
		Confirm: true,
		ConfirmPrompt: func(e browser.Entry) (string, string) {
			ce := e.(Entry)
			name := ce.T.Name
			if ce.T.IsDir {
				name += "/"
			}
			return fmt.Sprintf("Are you sure you want to delete %s?", name), ""
		},
		Run: func(e browser.Entry) tea.Cmd {
			ce := e.(Entry)
			pending := "Deleting " + ce.T.Name + "…"
			worker := doDelete(p, ce.T.Key, ce.T.Name)
			if ce.T.IsDir {
				pending = "Deleting " + ce.T.Name + "/…"
				worker = doDeletePrefix(p, ce.T.Key, ce.T.Name)
			}
			return tea.Batch(
				browser.SendMsg(browser.StatusMsg{Text: pending, Kind: browser.StatusInfo}),
				tea.Sequence(
					worker,
					browser.SendMsg(browser.RefreshMsg{}),
				),
			)
		},
	}
}

// doDeletePrefix runs the recursive prefix-delete via hf rm -R. On
// success it prunes every object with the matching prefix so the
// subsequent RefreshMsg sees the new state.
func doDeletePrefix(p *Provider, fullKey, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), folderDeleteTimeout)
		defer cancel()
		// Allowed gated this on owner == username, so the prefix
		// strip is safe.
		name := strings.TrimPrefix(fullKey, p.Client().Username+"/")
		if err := p.Client().DeletePrefix(ctx, name); err != nil {
			return browser.StatusMsg{
				Text: "Delete failed: " + err.Error(),
				Kind: browser.StatusError,
			}
		}
		p.RemovePrefix(fullKey)
		return browser.StatusMsg{
			Text: "Deleted " + displayName + "/",
			Kind: browser.StatusSuccess,
		}
	}
}

// doDelete runs the network call. On success it prunes the provider's
// listing so the subsequent RefreshMsg sees the new state.
func doDelete(p *Provider, fullKey, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancel()
		// Delete takes the bucket-relative filename within the user's
		// own prefix. Allowed gated this on owner == username, so the
		// prefix strip is safe.
		filename := strings.TrimPrefix(fullKey, p.Client().Username+"/")
		if err := p.Client().Delete(ctx, filename); err != nil {
			return browser.StatusMsg{
				Text: "Delete failed: " + err.Error(),
				Kind: browser.StatusError,
			}
		}
		p.Remove(fullKey)
		return browser.StatusMsg{
			Text: "Deleted " + displayName,
			Kind: browser.StatusSuccess,
		}
	}
}

// copyURLAction copies the bucket file's public URL. AppliesTo hides it
// for folders (no URL); Allowed handles the private-bucket case.
func copyURLAction() browser.Action {
	return browser.Action{
		Key:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy url")),
		AppliesTo: notDir,
		Allowed: func(e browser.Entry) (bool, string) {
			if ce, ok := e.(Entry); ok && ce.T.URL == "" {
				return false, "private bucket has no public URL — use d to download"
			}
			return true, ""
		},
		Run: func(e browser.Entry) tea.Cmd {
			ce := e.(Entry)
			return func() tea.Msg {
				if err := clipboard.WriteAll(ce.T.URL); err != nil {
					return browser.StatusMsg{
						Text: "Copy failed: " + err.Error(),
						Kind: browser.StatusError,
					}
				}
				return browser.StatusMsg{
					Text: "Copied URL for " + ce.T.Name,
					Kind: browser.StatusSuccess,
				}
			}
		},
	}
}

// downloadAction handles both files (fetch) and folders (sync). Because
// the semantics differ, AppliesTo is nil — applies to every entry — and
// Run branches on IsDir.
func downloadAction(client *cloud.Client) browser.Action {
	return browser.Action{
		Key: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download (folder = sync)")),
		Run: func(e browser.Entry) tea.Cmd {
			ce := e.(Entry)
			pending := "Downloading " + ce.T.Name + "…"
			if ce.T.IsDir {
				pending = "Syncing " + ce.T.Name + "/…"
			}
			return tea.Batch(
				browser.SendMsg(browser.StatusMsg{Text: pending, Kind: browser.StatusInfo}),
				doDownload(client, ce),
			)
		},
	}
}

func doDownload(client *cloud.Client, e Entry) tea.Cmd {
	if e.T.IsDir {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), folderTimeout)
			defer cancel()
			outDir := filepath.Base(e.T.Name)
			if err := client.Sync(ctx, e.T.Key, outDir); err != nil {
				return browser.StatusMsg{
					Text: "Download failed: " + err.Error(),
					Kind: browser.StatusError,
				}
			}
			return browser.StatusMsg{
				Text: "Downloaded " + outDir + "/",
				Kind: browser.StatusSuccess,
			}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		defer cancel()
		outPath := filepath.Base(e.T.Name)
		f, err := os.Create(outPath)
		if err != nil {
			return browser.StatusMsg{
				Text: "Download failed: " + err.Error(),
				Kind: browser.StatusError,
			}
		}
		defer func() { _ = f.Close() }()
		if err := client.DownloadByKey(ctx, e.T.Key, f); err != nil {
			return browser.StatusMsg{
				Text: "Download failed: " + err.Error(),
				Kind: browser.StatusError,
			}
		}
		return browser.StatusMsg{
			Text: "Downloaded " + outPath,
			Kind: browser.StatusSuccess,
		}
	}
}

// notDir is the standard "file-only" AppliesTo predicate.
func notDir(e browser.Entry) bool { return !e.IsDir() }
