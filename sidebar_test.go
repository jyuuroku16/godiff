package main

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeSelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	w.treeSel = "f:main.go"
	draft := ""
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, "Selected "+w.treeSel)
			w.fileTree(c)
			ui.TextInput(c, &draft).Label("Editor")
		})
	}, 300, 500)
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	old, ok := tt.Find("main.go")
	if !ok {
		t.Fatal("the selected file row is missing")
	}
	r, ok := tt.Find("old.txt")
	if !ok {
		t.Fatal("the file row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" || !tt.HasText("Selected f:old.txt") {
		t.Fatalf("selected %q, frame %q", w.treeSel, tt.Texts())
	}
	if !tt.Focused("Changed files") {
		t.Fatal("the file tree did not take focus on press")
	}
	pixel := func(r ui.Rect) color.RGBA {
		return tt.Image().RGBAAt(int(r.X+r.W-4), int(r.Y+r.H/2))
	}
	want := color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: 255}
	if pixel(old) == want || pixel(r) != want {
		t.Fatalf("old fill %v, new fill %v, want only the new file to have %v", pixel(old), pixel(r), want)
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" {
		t.Fatal("release changed the selection again")
	}
}

func TestFileTreeDirectoryDisclosureWaitsForRelease(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	r, ok := tt.Find("docs")
	if !ok {
		t.Fatal("the directory row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "d:docs" || w.closedDirs["d:docs"] {
		t.Fatal("press should select the directory and keep it open")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if !w.closedDirs["d:docs"] {
		t.Fatal("release did not close the directory")
	}
}

func TestHistorySelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.tab = 1
	tt.Frame()
	if err := tt.Click("Filter history"); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find("First commit")
	if !ok {
		t.Fatal("the commit row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.source.kind != sourceCommit || w.commit == nil || w.commit.Subject != "First commit" {
		t.Fatalf("press did not select the commit: %+v", w.source)
	}
	if !tt.Focused("History") {
		t.Fatal("the history did not take focus on press")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
}

func TestMultiProjectSwitchAndState(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)

	w, _ := newTestWindow(t, repo1)
	if len(w.projects) != 1 || !samePath(w.projects[0], repo1) {
		t.Fatalf("expected 1 project (%s), got: %v", repo1, w.projects)
	}

	// Add second project and switch to it
	if err := w.addProject(repo2, true); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}
	if len(w.projects) != 2 {
		t.Fatalf("expected 2 projects, got: %d", len(w.projects))
	}
	if !samePath(w.repo.Root, repo2) {
		t.Fatalf("expected active repo %s, got %s", repo2, w.repo.Root)
	}

	// Switch back to repo1
	if err := w.switchProject(repo1); err != nil {
		t.Fatalf("switchProject failed: %v", err)
	}
	if !samePath(w.repo.Root, repo1) {
		t.Fatalf("expected active repo %s, got %s", repo1, w.repo.Root)
	}

	// Remove repo2
	w.removeProject(repo2)
	if len(w.projects) != 1 || !samePath(w.projects[0], repo1) {
		t.Fatalf("expected 1 project remaining (%s), got: %v", repo1, w.projects)
	}
}

func TestProjectBarAndPopoverUI(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)

	w, tt := newTestWindow(t, repo1)
	if err := w.addProject(repo2, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}

	tt.Frame()
	name1 := filepath.Base(repo1)
	name2 := filepath.Base(repo2)

	// Ensure the project name appears in the sidebar top
	if !tt.HasText(name1) {
		t.Fatalf("project name %q not found in texts: %v", name1, tt.Texts())
	}

	// Ensure hover does not open popover anymore
	if w.projectPopoverOpen {
		t.Fatal("expected popover to be closed initially")
	}

	// Open popover by click/trigger
	w.projectPopoverOpen = true
	tt.Frame()

	if !tt.HasText("PROJECTS") {
		t.Fatalf("expected PROJECTS in popover, texts: %v", tt.Texts())
	}
	if !tt.HasText(name2) {
		t.Fatalf("expected secondary project name %q in popover", name2)
	}

	// Switch project via switchProject and render frame
	if err := w.switchProject(repo2); err != nil {
		t.Fatalf("switchProject failed: %v", err)
	}
	tt.Frame()
	if !samePath(w.repo.Root, repo2) {
		t.Fatalf("expected active repo %s, got %s", repo2, w.repo.Root)
	}
}

func TestProjectPersistence(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)

	// Save to state
	state.setProjects([]string{repo1, repo2})
	loaded := state.projects()
	if len(loaded) != 2 || !samePath(loaded[0], repo1) || !samePath(loaded[1], repo2) {
		t.Fatalf("unexpected persisted projects: %v", loaded)
	}

	// New window initializes with persisted projects
	w, _ := newTestWindow(t, repo1)
	if len(w.projects) != 2 {
		t.Fatalf("expected 2 projects restored, got: %d", len(w.projects))
	}
}

func TestInvalidProjectHandling(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)

	w, tt := newTestWindow(t, repo1)
	if err := w.addProject(repo2, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}

	// Simulate deletion of repo2
	if err := os.RemoveAll(repo2); err != nil {
		t.Fatalf("failed to remove repo2: %v", err)
	}

	// Verify isProjectValid detects deletion
	if isProjectValid(repo2) {
		t.Fatalf("expected deleted directory %s to be invalid", repo2)
	}

	// Open popover and render frame
	w.projectPopoverOpen = true
	tt.Frame()

	// Should show "已失效"
	if !tt.HasText("已失效") {
		t.Fatalf("expected '已失效' badge in popover texts: %v", tt.Texts())
	}

	// Attempting to switch to deleted repo2 must fail and remain on repo1
	err := w.switchProject(repo2)
	if err == nil {
		t.Fatal("expected switchProject to deleted repo to return error")
	}
	if !samePath(w.repo.Root, repo1) {
		t.Fatalf("expected to remain on repo1, got: %s", w.repo.Root)
	}

	// Removing the invalid project should succeed
	w.removeProject(repo2)
	if len(w.projects) != 1 || !samePath(w.projects[0], repo1) {
		t.Fatalf("expected repo2 removed from projects: %v", w.projects)
	}
}

func TestAddProjectsMultiple(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)
	repo3 := testRepo(t)

	w, _ := newTestWindow(t, repo1)
	if len(w.projects) != 1 {
		t.Fatalf("expected 1 project initially, got: %d", len(w.projects))
	}

	// Add multiple projects at once (including a non-git directory which should be skipped)
	nonGitDir := t.TempDir()
	err := w.addProjects([]string{repo2, nonGitDir, repo3})
	if err != nil {
		t.Fatalf("addProjects failed: %v", err)
	}

	// Should contain repo1, repo2, repo3 (total 3)
	if len(w.projects) != 3 {
		t.Fatalf("expected 3 valid projects, got: %d (%v)", len(w.projects), w.projects)
	}

	// Should switch to the last valid project (repo3)
	if !samePath(w.repo.Root, repo3) {
		t.Fatalf("expected active repo %s, got: %s", repo3, w.repo.Root)
	}
}

func TestProjectDragReorder(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)
	repo1 := testRepo(t)
	repo2 := testRepo(t)
	repo3 := testRepo(t)

	w, _ := newTestWindow(t, repo1)
	if err := w.addProject(repo2, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}
	if err := w.addProject(repo3, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}

	if len(w.projects) != 3 {
		t.Fatalf("expected 3 projects, got: %d", len(w.projects))
	}
	if !samePath(w.projects[0], repo1) || !samePath(w.projects[1], repo2) || !samePath(w.projects[2], repo3) {
		t.Fatalf("unexpected initial order: %v", w.projects)
	}

	// Move first item (repo1) to the end (index 2)
	w.reorderProject(0, 2)

	if !samePath(w.projects[0], repo2) || !samePath(w.projects[1], repo3) || !samePath(w.projects[2], repo1) {
		t.Fatalf("unexpected order after reorder(0, 2): %v", w.projects)
	}

	// Verify persistence in state
	persisted := state.projects()
	if len(persisted) != 3 || !samePath(persisted[0], repo2) || !samePath(persisted[1], repo3) || !samePath(persisted[2], repo1) {
		t.Fatalf("persisted projects mismatch: %v", persisted)
	}

	// Move last item back to first
	w.reorderProject(2, 0)
	if !samePath(w.projects[0], repo1) || !samePath(w.projects[1], repo2) || !samePath(w.projects[2], repo3) {
		t.Fatalf("unexpected order after reorder(2, 0): %v", w.projects)
	}

	// Verify that drag interaction does not close popover
	w2, tt := newTestWindow(t, repo1)
	if err := w2.addProject(repo2, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}
	w2.projectPopoverOpen = true
	tt.Frame()

	// Simulate drag active and ending
	w2.projectDragID = repo1
	w2.projectDragging = true
	w2.projectDragDY = 25
	tt.Frame()
	if !w2.projectPopoverOpen {
		t.Fatal("expected popover to remain open while dragging")
	}

	// Simulate drag release with cooldown
	w2.projectDragging = false
	w2.projectDragID = ""
	w2.projectDragCooldown = 2
	tt.Frame()
	if !w2.projectPopoverOpen {
		t.Fatal("expected popover to remain open after drag ends")
	}
}

func TestSidebarProjectsSplitAndSwap(t *testing.T) {
	state.setProjects(nil)
	defer state.setProjects(nil)

	repo1 := testRepo(t)
	repo2 := testRepo(t)

	w, tt := newTestWindow(t, repo1)
	if err := w.addProject(repo2, false); err != nil {
		t.Fatalf("addProject failed: %v", err)
	}

	// 1. Verify defaults
	if !w.sidebarProjectsShown {
		t.Fatal("expected sidebarProjectsShown to default to true")
	}
	if !w.sidebarProjectsOnTop {
		t.Fatal("expected sidebarProjectsOnTop to default to true")
	}

	tt.Frame()

	// 2. In split mode, PROJECTS header is shown, bottom "Open Folder…" row is hidden, + button is present
	if !tt.HasText("PROJECTS") {
		t.Fatalf("expected 'PROJECTS' in sidebar split, texts: %v", tt.Texts())
	}
	if _, ok := tt.Find("Open Folder"); !ok {
		t.Fatal("expected Open Folder (+) button next to PROJECTS header")
	}
	if tt.HasText("LOADED PROJECTS") {
		t.Fatal("unexpected 'LOADED PROJECTS' found")
	}
	if tt.HasText("Open Folder…") {
		t.Fatal("expected bottom 'Open Folder…' row to be hidden in split mode")
	}

	// 3. Toggle off: switch to popover mode
	w.sidebarProjectsShown = false
	tt.Frame()

	// In non-split mode, projectBar is shown (showing base name of repo)
	base := filepath.Base(repo1)
	if !tt.HasText(base) {
		t.Fatalf("expected projectBar with name %s, texts: %v", base, tt.Texts())
	}

	// 4. Toggle back on
	w.sidebarProjectsShown = true
	tt.Frame()
	if !tt.HasText("PROJECTS") {
		t.Fatal("expected 'PROJECTS' restored in split mode")
	}

	// 5. Test Swap button click and transition animation
	if err := tt.Click("Swap sections"); err != nil {
		t.Fatalf("failed to click swap button: %v", err)
	}
	if w.sidebarProjectsOnTop {
		t.Fatal("expected sidebarProjectsOnTop to be false after swap click")
	}
	if w.swapAnimStart.IsZero() {
		t.Fatal("expected swapAnimStart to be set for transition animation")
	}

	// Run animation frames
	for frame := 0; frame < 5; frame++ {
		tt.Frame()
	}

	// 6. Test Persistence
	state.setSidebarProjects(false, 310, false)
	shown, height, onTop := state.sidebarProjects()
	if shown || height != 310 || onTop {
		t.Fatalf("unexpected state: shown=%v, height=%f, onTop=%v", shown, height, onTop)
	}

	// Restore default state for other tests
	state.setSidebarProjects(true, 220, true)
}

