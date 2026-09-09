// Package assets contains immutable product-owned visual assets embedded in
// the Hermetrix binary. Keeping the canonical brand files here makes the
// source tree, desktop packaging, and HTTP surface share one identity.
package assets

import "embed"

// Files contains the brand and shared UI icon directories. Keeping the line
// icons in the same immutable product bundle lets browser and desktop mode use
// one visual vocabulary without a CDN or a runtime dependency.
//
//go:embed brand/* icons/*
var Files embed.FS
