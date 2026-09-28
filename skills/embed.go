// Package skills embeds the agent skill that `cadence skill install` writes.
package skills

import _ "embed"

// Cadence is skills/cadence/SKILL.md.
//
//go:embed cadence/SKILL.md
var Cadence []byte
