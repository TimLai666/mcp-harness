package harness

import (
	"strings"
	"testing"
)

func TestEmitUnifiedHunksContextAndMarkers(t *testing.T) {
	a := []string{"l1", "l2", "l3", "l4", "l5"}
	b := []string{"l1", "l2", "CHG", "l4", "l5"}
	var sb strings.Builder
	emitUnifiedHunks(a, b, 1, func(s string) { sb.WriteString(s) })
	out := sb.String()
	if !strings.Contains(out, "@@ -2,3 +2,3 @@") {
		t.Fatalf("unexpected hunk header:\n%s", out)
	}
	if !strings.Contains(out, " l2\n") || !strings.Contains(out, "-l3\n") || !strings.Contains(out, "+CHG\n") || !strings.Contains(out, " l4\n") {
		t.Fatalf("expected context + change lines, got:\n%s", out)
	}
	if strings.Contains(out, "l1") {
		t.Fatalf("context=1 should not include l1:\n%s", out)
	}
}
