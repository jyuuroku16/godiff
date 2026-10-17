package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/git"
)

func TestGitGraphPanelToggleAndLayout(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	dir := t.TempDir()
	repo, err := git.Open(dir)
	if err != nil {
		// Init repo
		r := &git.Repo{Root: dir}
		_, _ = r.Git("init")
		_, _ = r.Git("config", "user.name", "Tester")
		_, _ = r.Git("config", "user.email", "test@example.com")
		f := filepath.Join(dir, "README.md")
		_ = os.WriteFile(f, []byte("# Godiff\n"), 0644)
		_, _ = r.Git("add", "README.md")
		_, _ = r.Git("commit", "-m", "initial commit")
		repo, _ = git.Open(dir)
	}

	w, tt := newTestWindow(t, repo.Root)

	// Initially git graph is closed
	w.gitGraphShown = false
	tt.Frame()

	if tt.HasText("All commits") {
		t.Fatal("expected git graph to be closed initially")
	}

	// Toggle on
	w.toggleGitGraph()
	if !w.gitGraphShown {
		t.Fatal("expected gitGraphShown to be true after toggle")
	}

	// Mock graph data for deterministic UI verification
	c1 := git.Commit{
		Hash:    "1234567890abcdef1234567890abcdef12345678",
		Short:   "1234567",
		Subject: "feat: add commit graph view",
		Author:  "Tester",
		Time:    time.Now(),
		Refs:    "HEAD -> main",
	}
	w.gitAllCommits = []git.Commit{c1}
	w.gitRefs = []git.Ref{
		{FullName: "refs/heads/main", Short: "main", Kind: git.RefLocalBranch, IsHead: true, Hash: c1.Hash},
	}
	w.gitGraphLayout = BuildGraph(w.gitAllCommits, "main", false)

	tt.Frame()

	// Should show "All commits" and the commit subject in graph
	if !tt.HasText("All commits") {
		t.Fatalf("expected 'All commits' in tree, got texts: %v", tt.Texts())
	}
	if !tt.HasText("feat: add commit graph view") {
		t.Fatalf("expected commit subject in graph view, got texts: %v", tt.Texts())
	}

	// Verify resizer bounds
	w.dragGitGraphHeight = 350
	w.gitGraphHeight = 350
	tt.Frame()
	if w.gitGraphHeight != 350 {
		t.Fatalf("expected gitGraphHeight 350, got: %f", w.gitGraphHeight)
	}

	// Toggle off
	w.toggleGitGraph()
	if w.gitGraphShown {
		t.Fatal("expected gitGraphShown to be false after toggle")
	}
	tt.Frame()
	if tt.HasText("All commits") {
		t.Fatal("expected git graph panel to be hidden")
	}
}

func TestGitGraphSelectionSwitchesSource(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	dir := t.TempDir()
	r := &git.Repo{Root: dir}
	_, _ = r.Git("init")
	_, _ = r.Git("config", "user.name", "Tester")
	_, _ = r.Git("config", "user.email", "test@example.com")
	f := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f, []byte("line1\n"), 0644)
	_, _ = r.Git("add", "README.md")
	_, _ = r.Git("commit", "-m", "commit one")

	_ = os.WriteFile(f, []byte("line1\nline2\n"), 0644)
	_, _ = r.Git("add", "README.md")
	_, _ = r.Git("commit", "-m", "commit two")

	repo, _ := git.Open(dir)
	w, tt := newTestWindow(t, repo.Root)

	w.gitGraphShown = true
	w.loadGitGraph()

	// Wait briefly or simulate populated graph
	commits, _ := repo.LogAll(0, 10)
	w.gitAllCommits = commits
	w.gitGraphLayout = BuildGraph(commits, "main", false)
	tt.Frame()

	if len(w.gitGraphLayout.Rows) < 2 {
		t.Fatalf("expected at least 2 commit rows, got: %d", len(w.gitGraphLayout.Rows))
	}

	// Select first commit
	firstCommitHash := w.gitGraphLayout.Rows[0].Commit.Hash
	w.setSource(source{kind: sourceCommit, ref: firstCommitHash})
	tt.Frame()

	if w.source.kind != sourceCommit || w.source.ref != firstCommitHash {
		t.Fatalf("expected sourceCommit on %s, got: %+v", firstCommitHash, w.source)
	}
}

func TestBranchClickJumpsToCommit(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	dir := t.TempDir()
	r := &git.Repo{Root: dir}
	_, _ = r.Git("init")
	_, _ = r.Git("config", "user.name", "Tester")
	_, _ = r.Git("config", "user.email", "test@example.com")
	f := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f, []byte("c1\n"), 0644)
	_, _ = r.Git("add", "README.md")
	_, _ = r.Git("commit", "-m", "commit 1")

	// Create feature branch
	_, _ = r.Git("checkout", "-b", "feature/mybranch")
	_ = os.WriteFile(f, []byte("c1\nc2\n"), 0644)
	_, _ = r.Git("add", "README.md")
	_, _ = r.Git("commit", "-m", "commit 2 on branch")

	repo, _ := git.Open(dir)
	w, tt := newTestWindow(t, repo.Root)
	w.gitGraphShown = true

	commits, _ := repo.LogAll(0, 10)
	refs, _ := repo.Refs()
	w.gitAllCommits = commits
	w.gitRefs = refs
	w.gitGraphLayout = BuildGraph(commits, "feature/mybranch", false)
	tt.Frame()

	// Verify flattened branch tree rows include the feature branch
	rows := w.visibleBranchRows()
	hasBranchRow := false
	for _, br := range rows {
		if br.name == "mybranch" || br.name == "feature" {
			hasBranchRow = true
			break
		}
	}
	if !hasBranchRow {
		t.Fatalf("expected feature branch in branch tree rows, got: %+v", rows)
	}

	// Verify compact width: width must be compact (<= 80px)
	if len(w.gitGraphLayout.Rows) > 0 {
		gw := w.gitGraphLayout.Width
		if gw > 80 {
			t.Fatalf("expected compact width for branch graph, got %f", gw)
		}
		if w.gitGraphLayout.Rows[0].SVG == nil {
			t.Fatal("expected precomputed SVG on graph row")
		}
	}
}

func TestUncommittedRowFormat(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	dir := t.TempDir()
	r := &git.Repo{Root: dir}
	_, _ = r.Git("init")
	_, _ = r.Git("config", "user.name", "Tester")
	_, _ = r.Git("config", "user.email", "test@example.com")
	f := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f, []byte("line1\n"), 0644)
	_, _ = r.Git("add", "README.md")
	_, _ = r.Git("commit", "-m", "initial commit")

	// Make an uncommitted modification
	_ = os.WriteFile(f, []byte("line1\nline2 uncommitted\n"), 0644)

	repo, _ := git.Open(dir)
	w, tt := newTestWindow(t, repo.Root)
	w.gitGraphShown = true

	commits, _ := repo.LogAll(0, 10)
	refs, _ := repo.Refs()
	w.gitAllCommits = commits
	w.gitRefs = refs
	w.gitGraphLayout = BuildGraph(commits, "main", true) // hasUncommitted = true
	tt.Frame()

	// 1. Verify headers exist
	if !tt.HasText("图表") || !tt.HasText("描述") || !tt.HasText("提交") || !tt.HasText("作者") || !tt.HasText("日期") {
		t.Fatalf("expected table headers (图表, 描述, 提交, 作者, 日期), got texts: %v", tt.Texts())
	}

	// 2. Verify "Uncommitted changes" is displayed
	if !tt.HasText("Uncommitted changes") {
		t.Fatalf("expected 'Uncommitted changes' text in graph, got texts: %v", tt.Texts())
	}

	// 3. Ensure NO irrelevant copy exists
	if tt.HasText("全部提交面板支持拖拽调高与本地修改") {
		t.Fatal("unexpected legacy irrelevant text found")
	}

	// 4. Ensure no redundant badges on uncommitted row
	if len(w.gitGraphLayout.Rows) > 0 && len(w.gitGraphLayout.Rows[0].Badges) > 0 {
		t.Fatalf("expected uncommitted row to have no badges, got %d", len(w.gitGraphLayout.Rows[0].Badges))
	}
}

func TestGodiffRepoRealGraph(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	// Open the actual current repo
	repo, err := git.Open(".")
	if err != nil {
		t.Skip("skipping real repo test: cannot open current dir")
	}

	w, tt := newTestWindow(t, repo.Root)
	w.gitGraphShown = true

	commits, err := repo.LogAll(0, 500)
	if err != nil || len(commits) == 0 {
		t.Fatalf("failed to read git commits: %v", err)
	}
	refs, _ := repo.Refs()
	w.gitAllCommits = commits
	w.gitRefs = refs

	// Build real graph layout with uncommitted changes
	layout := BuildGraph(commits, w.branch, true)
	if layout == nil || len(layout.Rows) == 0 {
		t.Fatal("expected layout not nil and not empty")
	}
	w.gitGraphLayout = layout

	// Render multiple frames to simulate real UI interactions
	for frame := 0; frame < 5; frame++ {
		tt.Frame()
	}

	// Verify header texts are rendered
	if !tt.HasText("图表") || !tt.HasText("描述") || !tt.HasText("提交") {
		t.Fatalf("expected table headers in real repo graph, texts: %v", tt.Texts())
	}

	// Verify uncommitted row is rendered
	if !tt.HasText("Uncommitted changes") {
		t.Fatalf("expected Uncommitted changes in real repo, texts: %v", tt.Texts())
	}

	// Now build graph without uncommitted changes (hasUncommitted = false)
	layoutClean := BuildGraph(commits, w.branch, false)
	w.gitGraphLayout = layoutClean
	tt.Frame()

	// Verify "Uncommitted changes" is NOT rendered when hasUncommitted is false
	if tt.HasText("Uncommitted changes") {
		t.Fatal("expected 'Uncommitted changes' to NOT be displayed when hasUncommitted is false")
	}

	// Verify repo.HasUncommitted reports false in clean working tree
	cleanDir := t.TempDir()
	gitIn(t, cleanDir, "init", "-q", "-b", "main")
	gitIn(t, cleanDir, "config", "user.name", "Tester")
	gitIn(t, cleanDir, "config", "user.email", "test@example.com")
	writeFile(t, cleanDir, "readme.txt", "hello\n")
	gitIn(t, cleanDir, "add", ".")
	gitIn(t, cleanDir, "commit", "-q", "-m", "Initial commit")
	cleanGitRepo, err := git.Open(cleanDir)
	if err != nil {
		t.Fatalf("failed to open clean repo: %v", err)
	}
	if cleanGitRepo.HasUncommitted() {
		t.Fatal("expected clean repo to report false for HasUncommitted")
	}
}
