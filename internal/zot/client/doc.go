// Package client contains the generated Zotero Web API client.
//
// DO NOT EDIT zotero.gen.go by hand — regenerate with `just zot-gen`. That
// recipe preprocesses the OpenAPI 3.1 spec at
// /Users/esh/Documents/webapps/apis/zotero/openapi.yaml into the 3.0 form
// oapi-codegen v2 understands, pipes it through scripts/zotero-mirror-paths.yq
// to duplicate every /users/{userID}/… path as a parallel /groups/{groupID}/…
// twin (the personal-vs-shared library split depends on this; the sole
// exception is /users/{userID}/groups itself), then runs oapi-codegen against
// internal/zot/client/config.yaml. If the upstream spec grows new 3.1-isms or
// name collisions, extend the sd/yq pipeline in the justfile rather than
// hand-editing the generated output.
//
// To add an endpoint: add its path to the OpenAPI spec, run just zot-gen (the
// transform auto-produces the group-path twin unless the path name says it
// shouldn't), then add a typed wrapper in internal/zot/api. That api package is
// also the one callers should use — never this package directly: api wraps the
// generated client with API-key auth, rate-limit backoff, and
// optimistic-concurrency retry.
package client
