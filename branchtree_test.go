package main

import (
	"testing"

	"github.com/egoist/godiff/internal/git"
)

func TestBuildBranchTrees(t *testing.T) {
	refs := []git.Ref{
		{FullName: "refs/heads/main", Short: "main", Kind: git.RefLocalBranch, IsHead: true},
		{FullName: "refs/heads/feature/ui", Short: "feature/ui", Kind: git.RefLocalBranch},
		{FullName: "refs/heads/feature/api", Short: "feature/api", Kind: git.RefLocalBranch},
		{FullName: "refs/remotes/origin/main", Short: "origin/main", Kind: git.RefRemoteBranch},
		{FullName: "refs/remotes/origin/feature/ui", Short: "origin/feature/ui", Kind: git.RefRemoteBranch},
		{FullName: "refs/tags/v1.0", Short: "v1.0", Kind: git.RefTag},
	}

	branches, remotes, tags := BuildBranchTrees(refs, "main", "")

	// Branches should contain "main" (leaf) and "feature" (folder)
	if len(branches) != 2 {
		t.Fatalf("expected 2 branch root items, got: %d", len(branches))
	}
	if branches[0].Name != "feature" && branches[1].Name != "feature" {
		t.Errorf("expected 'feature' folder in branches")
	}

	// Tags
	if len(tags) != 1 || tags[0].Name != "v1.0" {
		t.Fatalf("unexpected tags: %+v", tags)
	}

	// Remotes
	if len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Fatalf("unexpected remotes: %+v", remotes)
	}
	if len(remotes[0].Children) != 2 {
		t.Fatalf("expected 2 items under origin, got: %d", len(remotes[0].Children))
	}

	// Filter test
	fBranches, fRemotes, _ := BuildBranchTrees(refs, "main", "ui")
	if len(fBranches) != 1 || fBranches[0].Name != "feature" {
		t.Errorf("expected only 'feature' folder with 'ui' matched, got: %v", fBranches)
	}
	if len(fRemotes) != 1 || fRemotes[0].Name != "origin" {
		t.Errorf("expected origin matched, got: %v", fRemotes)
	}
}
