package cmd

import (
	"strings"
	"testing"
)

// machineOutputs is every command shape that has json and csv output.
var machineOutputs = [][]string{
	{"show", projFlag, e2eBetaID},
	{"show", projFlag, e2eBetaID, "--messages"},
	{"list", projFlag},
	{"summary", projFlag},
	{"summary", projFlag, "-d"},
	{"summary", projFlag, "-d", "--expand-agents"},
	{"global"},
}

// TestE2EMachineOutputEndsInOneNewline: a second newline after the last csv
// record reads as an empty record to csv.reader, and as a blank last line to
// tail and wc.
func TestE2EMachineOutputEndsInOneNewline(t *testing.T) {
	setupE2EFixture(t)
	for _, args := range machineOutputs {
		for _, format := range []string{"json", "csv"} {
			out, _, err := executeCLISplit(t, append(args, "-f", format)...)
			if err != nil {
				t.Fatalf("%v -f %s: %v", args, format, err)
			}
			if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
				t.Errorf("%v -f %s: want exactly one trailing newline, got ending %q", args, format, out[max(0, len(out)-20):])
			}
		}
	}
}
