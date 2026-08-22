// Package assets contains immutable product-owned visual assets embedded in
// the Hermetrix binary. Keeping the canonical brand files here makes the
// source tree, desktop packaging, and HTTP surface share one identity.
package assets

import "embed"

// Files contains the brand directory at paths such as brand/hermetrix-mark.png.
//
//go:embed brand/*
var Files embed.FS
