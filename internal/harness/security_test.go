package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInsideRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveInside(root, filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestResolveReferencesInjectsSmallTextFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := ResolveReferences("read @README.md", Workspace{Root: root, Mode: ModeWork}, 40000)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if !refs[0].Complete || refs[0].Content != "hello" {
		t.Fatalf("unexpected ref: %#v", refs[0])
	}
}
