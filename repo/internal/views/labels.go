package views

import "strings"

// HumanLabel converts a raw snake_case enum value (e.g. "pending_confirmation",
// "checked_in", "group_buy_threshold_met") into a human-readable Title Case
// label suitable for direct rendering in the UI ("Pending Confirmation",
// "Checked In", "Group Buy Threshold Met").
//
// This is the single presentation mapping point — every templ template that
// needs to display a database enum should call this rather than embedding
// `string(value)` directly, so the on-screen wording stays consistent across
// pages and is decoupled from the technical column values.
func HumanLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '_' || r == '-'
	})
	for i, p := range parts {
		if p == "" {
			continue
		}
		// Title-case the first letter; leave the rest of the word as-is so
		// short acronyms like "id" remain "Id" (good enough for UI labels).
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
