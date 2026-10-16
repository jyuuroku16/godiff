package main

import (
	"testing"
	"time"

	"github.com/egoist/godiff/internal/git"
)

func TestBuildGraph(t *testing.T) {
	// Scenario:
	// C3 (merge of C2 and C1)
	// |\
	// | C2
	// |/
	// C1
	c1 := git.Commit{
		Hash:    "1111111111111111111111111111111111111111",
		Short:   "1111111",
		Subject: "Commit 1",
		Author:  "Tester",
		Time:    time.Now().Add(-3 * time.Hour),
	}
	c2 := git.Commit{
		Hash:    "2222222222222222222222222222222222222222",
		Short:   "2222222",
		Parents: []string{"1111111111111111111111111111111111111111"},
		Subject: "Commit 2 (branch)",
		Author:  "Tester",
		Time:    time.Now().Add(-2 * time.Hour),
		Refs:    "feature",
	}
	c3 := git.Commit{
		Hash:    "3333333333333333333333333333333333333333",
		Short:   "3333333",
		Parents: []string{"1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"},
		Subject: "Commit 3 (merge)",
		Author:  "Tester",
		Time:    time.Now().Add(-1 * time.Hour),
		Refs:    "HEAD -> main, tag: v1.0",
	}

	commits := []git.Commit{c3, c2, c1}
	layout := BuildGraph(commits, "main", true)

	if layout == nil {
		t.Fatal("expected layout not nil")
	}

	// 1 uncommitted row + 3 commits = 4 rows
	if len(layout.Rows) != 4 {
		t.Fatalf("expected 4 rows, got: %d", len(layout.Rows))
	}

	// First row is uncommitted
	if !layout.Rows[0].IsUncommitted {
		t.Errorf("expected first row to be uncommitted")
	}

	// Second row is C3 (merge)
	rMerge := layout.Rows[1]
	if rMerge.Commit.Hash != c3.Hash {
		t.Errorf("expected C3, got: %s", rMerge.Commit.Hash)
	}
	if len(rMerge.Badges) != 2 {
		t.Errorf("expected 2 badges (main, v1.0), got: %d", len(rMerge.Badges))
	}

	// C3 has 2 outgoing lines because it merges 2 parents
	outCount := 0
	for _, ln := range rMerge.Lines {
		if ln.Kind == LineOut {
			outCount++
		}
	}
	if outCount != 2 {
		t.Errorf("expected 2 outgoing lines for merge commit, got: %d", outCount)
	}

	// Verify MaxCols >= 2
	if layout.MaxCols < 2 {
		t.Errorf("expected MaxCols >= 2, got: %d", layout.MaxCols)
	}
}

func TestParseRefBadges(t *testing.T) {
	raw := "HEAD -> main, origin/main, tag: v2.0, feature/login"
	badges, isHead := parseRefBadges(raw, "main")
	if !isHead {
		t.Errorf("expected isHead to be true")
	}
	if len(badges) != 4 {
		t.Fatalf("expected 4 badges, got: %d", len(badges))
	}

	if badges[0].Name != "main" || badges[0].Kind != BadgeLocalBranch {
		t.Errorf("unexpected badge 0: %+v", badges[0])
	}
	if badges[1].Name != "origin/main" || badges[1].Kind != BadgeRemoteBranch {
		t.Errorf("unexpected badge 1: %+v", badges[1])
	}
	if badges[2].Name != "v2.0" || badges[2].Kind != BadgeTag {
		t.Errorf("unexpected badge 2: %+v", badges[2])
	}
	if badges[3].Name != "feature/login" || badges[3].Kind != BadgeLocalBranch {
		t.Errorf("unexpected badge 3: %+v", badges[3])
	}
}
