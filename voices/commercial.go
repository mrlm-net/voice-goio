//go:build !commercial

package voices

// commercialDefault is off for a personal build: every downloadable voice is
// assignable regardless of its licence.
//
// SPEC.md 10 leaves distribution open. Building with -tags commercial flips
// this, and the pool then only assigns models whose MODEL_CARD licence appears
// in permissiveLicences. The decision is a build flag rather than a runtime
// setting so a shipped binary cannot be configured into a licence breach.
const commercialDefault = false
