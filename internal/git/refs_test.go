package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRefsAndLogAll(t *testing.T) {
	dir := t.TempDir()
	repo := &Repo{Root: dir}

	runGit := func(args ...string) {
		t.Helper()
		if _, err := repo.Git(args...); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	runGit("init")
	runGit("config", "user.name", "Tester")
	runGit("config", "user.email", "test@example.com")

	// Commit 1
	file1 := filepath.Join(dir, "file1.txt")
	_ = os.WriteFile(file1, []byte("hello\n"), 0644)
	runGit("add", "file1.txt")
	runGit("commit", "-m", "first commit")

	// Create tag
	runGit("tag", "v1.0")

	// Branch feature
	runGit("checkout", "-b", "feature/foo")
	file2 := filepath.Join(dir, "file2.txt")
	_ = os.WriteFile(file2, []byte("world\n"), 0644)
	runGit("add", "file2.txt")
	runGit("commit", "-m", "feature commit")

	// Test Refs
	refs, err := repo.Refs()
	if err != nil {
		t.Fatalf("Refs() error: %v", err)
	}

	hasFeature := false
	hasTag := false
	for _, r := range refs {
		if r.Short == "feature/foo" && r.Kind == RefLocalBranch {
			hasFeature = true
			if !r.IsHead {
				t.Errorf("expected feature/foo to be HEAD")
			}
		}
		if r.Short == "v1.0" && r.Kind == RefTag {
			hasTag = true
		}
	}
	if !hasFeature {
		t.Errorf("feature/foo branch not found in refs: %+v", refs)
	}
	if !hasTag {
		t.Errorf("v1.0 tag not found in refs: %+v", refs)
	}

	// Test LogAll
	commits, err := repo.LogAll(0, 50)
	if err != nil {
		t.Fatalf("LogAll() error: %v", err)
	}
	if len(commits) < 2 {
		t.Fatalf("expected at least 2 commits, got: %d", len(commits))
	}
	if commits[0].Subject != "feature commit" {
		t.Errorf("expected first commit to be 'feature commit', got: %s", commits[0].Subject)
	}
}
