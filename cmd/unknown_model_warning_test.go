package cmd

import (
	"strings"
	"testing"
)

// TestUnknownModelWarning: the warning names the build and where a newer one
// is, since prices only change with a new release.
func TestUnknownModelWarning(t *testing.T) {
	const rate = " priced at fallback $3/$15 per MTok"
	for _, tc := range []struct {
		name   string
		models []string
		ver    string
		want   string
	}{
		{"none", nil, "0.53.0", ""},
		{"one", []string{"claude-nova-6"}, "0.53.0",
			`Warning: unknown model "claude-nova-6"` + rate + "\n" +
				"  ficha 0.53.0 has no price for it. A newer release may know it: https://github.com/bardisty/ficha/releases\n"},
		{"two", []string{"claude-nova-6", "claude-zeta-9"}, "0.53.0",
			"Warning: 2 unknown models" + rate + ": claude-nova-6, claude-zeta-9\n" +
				"  ficha 0.53.0 has no price for them. A newer release may know them: https://github.com/bardisty/ficha/releases\n"},
		{"dev build", []string{"claude-nova-6"}, "dev",
			`Warning: unknown model "claude-nova-6"` + rate + "\n" +
				"  This development build of ficha has no price for it. Add it to modelCatalog in internal/pricing/pricing.go.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			writeUnknownModels(&sb, tc.models, tc.ver)
			if got := sb.String(); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
