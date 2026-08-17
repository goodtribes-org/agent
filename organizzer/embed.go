// Package organizzer holds the assets baked into the image at build time.
//
// go:embed cannot reach outside the directory of the file that declares it,
// so the embeds live at the module root and the packages that need them
// import this one. Prompts and guides are baked rather than fetched so a
// GitHub outage cannot change how the workers reason; per-repo context is
// fetched live, because that genuinely does change between runs.
package organizzer

import "embed"

// Prompts holds the system and per-stage prompt templates under prompts/.
//
//go:embed prompts
var Prompts embed.FS

// Guides holds the company context under guides/: a copy of the working
// root's CLAUDE.md, the security invariants, and the per-stack build gates.
//
//go:embed guides
var Guides embed.FS
