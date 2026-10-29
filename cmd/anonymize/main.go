// Command anonymize writes an anonymized copy of a FIT file, or of a Garmin Connect
// GPX or TCX export, for the test corpus. See package anonymize for what changes.
//
//	anonymize in.fit|in.gpx|in.tcx out
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iskyd/kudos/internal/anonymize"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: anonymize in.fit|in.gpx|in.tcx out")
		os.Exit(2)
	}
	st, err := run(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "anonymize:", err)
		os.Exit(1)
	}
	fmt.Printf("shifted %d longitudes (%d in unprofiled fields), replaced %d serial numbers, blanked %d texts\n",
		st.Longitudes+st.Unprofiled, st.Unprofiled, st.Serials, st.Texts)
}

func run(inPath, outPath string) (anonymize.Stats, error) {
	in, err := os.ReadFile(inPath) //nolint:gosec // G703: the path is the operator's own command-line argument
	if err != nil {
		return anonymize.Stats{}, fmt.Errorf("reading input: %w", err)
	}
	var (
		out []byte
		st  anonymize.Stats
	)
	switch strings.ToLower(filepath.Ext(inPath)) {
	case ".gpx", ".tcx":
		out, st, err = anonymize.XML(in)
	default:
		var buf bytes.Buffer
		st, err = anonymize.FIT(bytes.NewReader(in), &buf)
		out = buf.Bytes()
	}
	if err != nil {
		return st, fmt.Errorf("anonymizing: %w", err)
	}
	if err := os.WriteFile(outPath, out, 0o600); err != nil { //nolint:gosec // G703: the path is the operator's own command-line argument
		return st, fmt.Errorf("writing output: %w", err)
	}
	return st, nil
}
