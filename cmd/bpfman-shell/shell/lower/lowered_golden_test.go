package lower_test

// Golden test for the lowerer. testdata/language.bpfman is a single
// dense, valid program covering each construct that lowers to a
// distinct IR shape; testdata/language.lowered is its expected lowered
// form. The test lowers the fixture through the driver's
// parse/expand/lower path (the same path `bpfman-shell --lowered`
// uses, so imports are expanded) and compares against the golden.
//
// Regenerate the golden after an intended lowerer change with:
//
//	make update-lowered-goldens
//
// It lives in an external test package so it can import driver, whose
// import-expansion the imported-def case needs; lowering itself is
// pure, so the test runs in the ordinary host-side lane.

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
)

var updateGolden = flag.Bool("update", false, "rewrite lowered golden files instead of comparing")

func TestLanguageLoweredGolden(t *testing.T) {
	t.Parallel()

	const (
		src    = "testdata/language.bpfman"
		golden = "testdata/language.lowered"
	)

	reader, err := driver.OpenScriptReader(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer reader.Close()

	var out, errOut bytes.Buffer
	if driver.LoweredInput(reader, &out, &errOut, src) {
		t.Fatalf("lowering %s reported an issue: %s", src, errOut.String())
	}

	if *updateGolden {
		if err := os.WriteFile(filepath.Clean(golden), out.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", golden, err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with `make update-lowered-goldens`): %v", golden, err)
	}
	if got := out.String(); got != string(want) {
		t.Fatalf("lowered IR for %s does not match %s.\nRegenerate with `make update-lowered-goldens` if the change is intended.\n\n--- got ---\n%s\n--- want ---\n%s", src, golden, got, want)
	}
}
