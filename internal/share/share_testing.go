package share

// share_testing.go — the swap helpers cmd/sci's `sci cloud put` tests use to
// drive the upload path against a recorder instead of the SciMinds buckets.
// Intentionally exported so a cross-package test can call them; production
// never does.

// SetPutClientFactory redirects [CheckExists] and [Share] at fn instead of
// authenticating with `hf` and talking to a real bucket.
//
// Production never calls it: it is the seam that lets cmd/sci drive
// `sci cloud put` end-to-end and assert that a refused overwrite reached the
// existence check and *nothing else*. Pair every call with a deferred
// [ResetPutClientFactory] so package state stays clean across tests.
func SetPutClientFactory(fn func(bucket string) (PutClient, error)) { newPutClient = fn }

// ResetPutClientFactory restores the real `hf`-backed client factory. The
// test-only counterpart to [SetPutClientFactory]; defer it.
func ResetPutClientFactory() { newPutClient = defaultPutClient }
