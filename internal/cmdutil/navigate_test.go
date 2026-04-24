package cmdutil

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// --- RejectUnknownSubcommand ---

func TestRejectUnknownSubcommand_PassesOnEmpty(t *testing.T) {
	t.Parallel()
	// Build an isolated command and drive it through root.Run so the
	// Before hook gets a cmd whose parsedArgs are actually set (the
	// struct is otherwise populated mid-Run by urfave).
	var fired bool
	root := &cli.Command{
		Name: "test",
		Commands: []*cli.Command{
			{Name: "child", Action: func(context.Context, *cli.Command) error { return nil }},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			fired = true
			return RejectUnknownSubcommand(ctx, cmd)
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	err := root.Run(context.Background(), []string{"test"})
	if !fired {
		t.Fatal("Before hook never fired")
	}
	if err != nil {
		t.Errorf("empty args: expected nil, got %v", err)
	}
}

func TestRejectUnknownSubcommand_PassesOnKnownChild(t *testing.T) {
	t.Parallel()
	var sawChild bool
	root := &cli.Command{
		Name:   "test",
		Before: RejectUnknownSubcommand,
		Commands: []*cli.Command{
			{
				Name: "child",
				Action: func(context.Context, *cli.Command) error {
					sawChild = true
					return nil
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"test", "child"})
	if err != nil {
		t.Errorf("known child should not error, got: %v", err)
	}
	if !sawChild {
		t.Error("known child's Action was not reached")
	}
}

func TestRejectUnknownSubcommand_ErrorsOnUnknown(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := &cli.Command{
		Name:   "test",
		Writer: &buf,
		Before: RejectUnknownSubcommand,
		Commands: []*cli.Command{
			{Name: "realone", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	err := root.Run(context.Background(), []string{"test", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown command") || !strings.Contains(msg, `"bogus"`) {
		t.Errorf("error should identify the unknown command, got: %v", err)
	}
	// Help dump uses a custom printer; bare urfave output goes to root.Writer
	// but the template includes the command name and a usage line. Confirming
	// the writer saw something is enough — we don't pin the exact layout.
	if buf.Len() == 0 {
		t.Error("help should have been dumped to the writer")
	}
}

// --- ChainBefore ---

func TestChainBefore_RunsInOrderUntilError(t *testing.T) {
	t.Parallel()
	var order []string
	a := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		order = append(order, "a")
		return ctx, nil
	}
	b := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		order = append(order, "b")
		return ctx, errors.New("b failed")
	}
	c := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		order = append(order, "c")
		return ctx, nil
	}
	chained := ChainBefore(a, b, c)
	_, err := chained(context.Background(), &cli.Command{})
	if err == nil || err.Error() != "b failed" {
		t.Errorf("expected 'b failed', got %v", err)
	}
	if strings.Join(order, ",") != "a,b" {
		t.Errorf("c should not have run; order=%v", order)
	}
}

func TestChainBefore_SkipsNilEntries(t *testing.T) {
	t.Parallel()
	var ran int
	hook := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		ran++
		return ctx, nil
	}
	chained := ChainBefore(nil, hook, nil, hook)
	_, err := chained(context.Background(), &cli.Command{})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 2 {
		t.Errorf("expected 2 hook invocations, got %d", ran)
	}
}

// ctxKey is a small typed key for the threading test.
type ctxKey struct{}

func TestChainBefore_ThreadsContext(t *testing.T) {
	t.Parallel()
	first := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		return context.WithValue(ctx, ctxKey{}, "value"), nil
	}
	var seen string
	second := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		if v, ok := ctx.Value(ctxKey{}).(string); ok {
			seen = v
		}
		return ctx, nil
	}
	_, err := ChainBefore(first, second)(context.Background(), &cli.Command{})
	if err != nil {
		t.Fatal(err)
	}
	if seen != "value" {
		t.Errorf("expected context threading to carry 'value', got %q", seen)
	}
}

// --- WireNamespaceDefaults ---

func TestWireNamespaceDefaults_WiresEveryNamespace(t *testing.T) {
	t.Parallel()
	leaf := &cli.Command{Name: "leaf"}
	inner := &cli.Command{Name: "inner", Commands: []*cli.Command{leaf}}
	outer := &cli.Command{Name: "outer", Commands: []*cli.Command{inner}}
	root := &cli.Command{Name: "root", Commands: []*cli.Command{outer}}

	WireNamespaceDefaults(root)

	// root, outer, inner all have Commands → all should be wired.
	// leaf has no Commands → should NOT be wired.
	for _, ns := range []*cli.Command{root, outer, inner} {
		if ns.Before == nil {
			t.Errorf("%s: expected Before to be set", ns.Name)
		}
	}
	if leaf.Before != nil {
		t.Error("leaf should not have been wired — it has no subcommands")
	}
}

func TestWireNamespaceDefaults_Idempotent(t *testing.T) {
	t.Parallel()
	var existingRan int
	existing := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		existingRan++
		return ctx, nil
	}
	root := &cli.Command{
		Name:   "root",
		Before: existing,
		Commands: []*cli.Command{
			{Name: "child", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}

	WireNamespaceDefaults(root)
	WireNamespaceDefaults(root) // second call should not re-wrap.

	err := root.Run(context.Background(), []string{"root", "child"})
	if err != nil {
		t.Fatal(err)
	}
	if existingRan != 1 {
		t.Errorf("existing Before should run exactly once per Run, got %d", existingRan)
	}
}

func TestWireNamespaceDefaults_PreservesExistingBefore(t *testing.T) {
	t.Parallel()
	var existingRan bool
	existing := func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		existingRan = true
		return ctx, nil
	}
	root := &cli.Command{
		Name:   "root",
		Before: existing,
		Commands: []*cli.Command{
			{Name: "child", Action: func(context.Context, *cli.Command) error { return nil }},
		},
	}
	WireNamespaceDefaults(root)

	if err := root.Run(context.Background(), []string{"root", "child"}); err != nil {
		t.Fatal(err)
	}
	if !existingRan {
		t.Error("existing Before was not preserved in the chain")
	}
}
