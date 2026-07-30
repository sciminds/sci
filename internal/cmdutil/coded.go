package cmdutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/sciminds/cli/internal/uikit"
)

// Code identifies why a command failed — or what a warning is about — in a
// closed, machine-branchable vocabulary. Agents branch on codes instead of
// string-sniffing messages, so the set is deliberately small and additions
// should be rare and deliberate. Codes partition by what the caller should do
// about it, not by severity.
type Code string

// The closed code vocabulary. CodeStaleLocal appears only in warnings; the
// rest appear in error envelopes.
const (
	// CodeUsage means the command line itself is malformed (bad args, missing
	// required flag). Rewrite the command and retry; exits 2.
	CodeUsage Code = "usage"
	// CodeConflict means two flags or arguments are mutually exclusive.
	// Rewrite the command and retry; exits 2.
	CodeConflict Code = "conflict"
	// CodeNotFound means a named thing (item, collection, cite key, file)
	// does not exist in the target scope.
	CodeNotFound Code = "not-found"
	// CodeAmbiguous means a name matched more than one thing and the command
	// refuses to guess.
	CodeAmbiguous Code = "ambiguous"
	// CodeOffline means the command needs the network and it is unavailable.
	CodeOffline Code = "offline"
	// CodeNotConfigured means setup is required before this command can run.
	CodeNotConfigured Code = "not-configured"
	// CodeStaleLocal flags a local mirror that is behind ground truth
	// (warning channel only — never an error).
	CodeStaleLocal Code = "stale-local"
	// CodeDuplicate flags two or more records that resolve to the same
	// underlying thing (warning channel only — sci reports duplicates,
	// it never resolves them).
	CodeDuplicate Code = "duplicate"
	// CodeRuntime is the default for failures with no more specific code:
	// the command was well-formed but the work itself failed.
	CodeRuntime Code = "runtime"
)

// CodedError is an error carrying a machine-branchable [Code] and optional
// agent-actionable remediation. Fix is a complete corrected command the
// caller can resubmit verbatim — attach one only when the correction is
// unambiguous. Try is prose guidance for when it isn't.
type CodedError struct {
	Code    Code
	Message string
	Fix     string
	Try     string
}

// Error returns the message alone; fix/try render separately per channel
// (JSON envelope fields, indented arrow lines for humans).
func (e *CodedError) Error() string { return e.Message }

// Coded builds a [CodedError] printf-style. Chain [CodedError.WithFix] /
// [CodedError.WithTry] to attach remediation:
//
//	return cmdutil.Coded(cmdutil.CodeConflict, "--fulltext and --remote are mutually exclusive").
//		WithTry("drop one of the two flags")
func Coded(code Code, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithFix attaches a complete corrected command (resubmit verbatim) and
// returns the error for chaining.
func (e *CodedError) WithFix(fix string) *CodedError {
	e.Fix = fix
	return e
}

// WithTry attaches prose guidance and returns the error for chaining.
func (e *CodedError) WithTry(try string) *CodedError {
	e.Try = try
	return e
}

// Warning is a data-quality or freshness caveat attached to a successful
// result. It rides the --json envelope's warnings array and renders as an
// indented ⚠ block for humans. Same contract as [CodedError]: Fix, when
// present, is a complete command to resubmit verbatim.
type Warning struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Warner is implemented by Results that carry warnings. [Output] merges the
// warnings into the JSON envelope and appends them to human output; commands
// opt in per result type, and an empty slice is equivalent to not
// implementing the interface at all.
type Warner interface {
	Warnings() []Warning
}

// WithWarnings attaches warnings to any Result without touching its type.
// The wrapper delegates JSON/Human to r and concatenates r's own warnings
// (when it implements [Warner]) with the extra ones. Passing no warnings
// returns r unchanged, so call sites can wrap unconditionally:
//
//	cmdutil.Output(cmd, cmdutil.WithWarnings(res, staleWarning(db)...))
func WithWarnings(r Result, warns ...Warning) Result {
	if len(warns) == 0 {
		return r
	}
	return warnedResult{Result: r, extra: warns}
}

// warnedResult is the wrapper type behind [WithWarnings].
type warnedResult struct {
	Result
	extra []Warning
}

// Warnings returns the wrapped Result's warnings followed by the extras.
func (w warnedResult) Warnings() []Warning {
	if inner, ok := w.Result.(Warner); ok {
		return append(slices.Clone(inner.Warnings()), w.extra...)
	}
	return w.extra
}

// errorBody is the error half of the --json envelope.
type errorBody struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
	Try     string `json:"try,omitempty"`
}

// HandleError renders a command failure and returns the process exit code.
// It is the single error sink for main(): in JSON mode it emits a one-line
// {"ok":false,"error":{code,message,fix,try}} envelope on stdout (agents read
// one stream); otherwise it prints the human ✗ line plus any fix/try arrows
// to stderr. Usage-class codes (usage, conflict) exit 2 — "rewrite the
// command and retry" — everything else exits 1.
func HandleError(err error, jsonMode bool) int {
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		coded = &CodedError{Code: CodeRuntime, Message: err.Error()}
	}

	if jsonMode {
		env := struct {
			OK    bool      `json:"ok"`
			Error errorBody `json:"error"`
		}{Error: errorBody{Code: coded.Code, Message: coded.Message, Fix: coded.Fix, Try: coded.Try}}
		// Match Output's encoder settings (no HTML escaping) but stay
		// single-line: one greppable envelope per failure.
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		if encErr := enc.Encode(env); encErr != nil {
			// Can't happen for this shape; fall back to a hand-built line so
			// the one-stream contract holds even if it somehow does.
			_, _ = fmt.Fprintf(os.Stdout, `{"ok":false,"error":{"code":"runtime","message":%q}}`+"\n", coded.Message)
		}
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", uikit.SymFail, coded.Message)
		if coded.Fix != "" {
			fmt.Fprintf(os.Stderr, "    %s fix: %s\n", uikit.SymArrow, coded.Fix)
		}
		if coded.Try != "" {
			fmt.Fprintf(os.Stderr, "    %s try: %s\n", uikit.SymArrow, coded.Try)
		}
	}

	if coded.Code == CodeUsage || coded.Code == CodeConflict {
		return 2
	}
	return 1
}
