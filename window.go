package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sourceKind is what a window reviews.
type sourceKind uint8

const (
	sourceWorkingTree sourceKind = iota // uncommitted changes
	sourceCommit                        // a commit, against its first parent
	sourceBranch                        // the work tree since it branched from a branch
	sourcePull                          // a pull request on GitHub, since it branched
)

type source struct {
	kind sourceKind
	ref  string // the commit, the branch, or the pull request as owner/repo#123
}

// historyPage is how many commits the History tab loads at a time: git
// lists hundreds in a few milliseconds, and the list builds only the rows
// in view.
const historyPage = 200

// session is the review of one source, kept while the window shows
// another.
type session struct {
	comments []*comment
	viewed   map[string]string
}

// projectState is the state of a project kept while the window shows
// another.
type projectState struct {
	repo      *git.Repo
	source    source
	branch    string
	viewed    map[string]string
	comments  []*comment
	tab       int
	filter    string
	sessions  map[source]*session
	analyses  map[source]*analysisState
	groups    []fileGroup
	groupPref groupMode
}

// window is a window reviewing a repository.
type window struct {
	win      *mygo.Window
	repo     *git.Repo
	launch   source // what the window was opened on
	source   source
	settings Settings

	// Projects loaded in this window.
	projects           []string
	projectStates      map[string]*projectState
	projectPopoverOpen bool
	projectDragID       string
	projectDragDY       float32
	projectDragging     bool
	projectDragCooldown int

	branch     string
	files      []*fileState
	commit     *git.Commit // the commit reviewed, for sourceCommit
	base       string      // the merge base, for sourceBranch and sourcePull
	loading    bool
	loadedOnce bool
	loadErr    error
	gen        int // counts loads, to drop the results of older ones
	genA       atomic.Int64
	// loadStart is when the load started.
	loadStart time.Time
	viewed    map[string]string
	// loadStatus says what a slow load does, as fetching a pull request.
	loadStatus string

	// The sidebar.
	sidebarShown bool
	sidebarWidth float32
	tab          int // 0 files, 1 history
	filter       string
	filterFocus  bool
	treeList     ui.ListState
	treeSel      string // the key of the row chosen
	treeRows     []treeRow
	closedDirs   map[string]bool
	treeItems    map[string]*treeNode
	treeRoots    []string
	history      []git.Commit
	historyLimit int
	historyMore  bool
	historyList  ui.ListState
	commitTimes  map[string]commitTime
	// historyFilter filters the History tab, apart from the files'.
	historyFilter  string
	historyLoading bool
	// reloaded are the files that changed in the last refresh.
	reloaded  map[string]bool
	dragWidth float32

	// The top Git graph and branch tree panel.
	gitGraphShown      bool
	gitGraphHeight     float32
	dragGitGraphHeight float32
	gitBranchWidth     float32
	dragGitBranchWidth float32
	gitAllCommits      []git.Commit
	gitRefs            []git.Ref
	gitGraphLayout     *GraphLayout
	gitGraphLoading    bool
	gitFilter          string
	gitFilterFocus     bool
	gitTreeCollapsed   map[string]bool
	gitGraphList       ui.ListState
	gitBranchTreeList  ui.ListState
	gitSelectedHash    string

	// The diff surface.
	rows       []row
	rowsDirty  bool
	list       ui.ListState
	current    int // the file shown at the top of the surface
	hscroll    map[string]float32
	revealedAt time.Time
	now        time.Time
	charW      float32
	charKey    string
	// The hunk chosen with j and k.
	selFile, selHunk int
	focusList        bool
	focusedOnce      bool
	focusHistory     bool
	switchedAt       time.Time // when the source last changed, for GODIFF_DEBUG
	debugPressed     bool
	// hold keeps the background work of tests in held, to run frames
	// while it waits.
	hold bool
	held []func()
	// typing is set while a field has the focus, whose keys are its own.
	typing bool

	// Find in diffs.
	finding     bool
	query       string
	matches     []match
	match       int
	matchesFor  string
	fileMatches map[int]bool

	// The reviews of the other sources.
	sessions map[source]*session

	// GitHub: the repository's remote there, its open pull requests, and
	// the pull requests reviewed, the one shown in pr.
	gh           ghRemote
	pulls        []github.Summary
	pullsLoaded  bool
	pullsLoading bool
	pullsErr     error
	pullsList    ui.ListState
	pullsFilter  string
	pr           *pullState
	prs          map[source]*pullState

	// The groups of the files: by kind, or as an agent's review has them.
	groupPref groupMode
	groups    []fileGroup
	// notes are the notes of the agent's review, by path, and noteSel the
	// one gone to last.
	notes   map[string][]*aiNote
	noteSel *aiNote
	// regroupPending groups the files again as the next frame starts.
	regroupPending bool
	// placed are the threads and notes the rows show under their lines,
	// as the rows are built: the others show at the end of their files.
	placed map[any]bool
	// The agents' reviews, by source.
	analyses map[source]*analysisState
	// Review comments, the one asked about discarding, and the git user.
	comments   []*comment
	discarding *comment
	user       string
	copied     string
	copiedAt   time.Time

	// Committing.
	commitOpen     bool
	subject        string
	body           string
	commitPaths    map[string]bool
	commitBusy     bool
	commitErr      string
	commitOutput   string
	commitDone     string // the hash committed
	committedFiles int
	commitFocus    bool

	// The command bar.
	paletteOpen  bool
	paletteQuery string
	paletteRow   int
	paletteList  ui.ListState
	// palettePointer is where the pointer was over the list.
	palettePointer [2]float32

	// The dialog opening a commit or a branch.
	dialog      dialogKind
	dialogOpen  bool
	dialogValue string
	dialogErr   string
	dialogBusy  bool

	// changed is set when the work tree changed since the last load.
	changed   bool
	signature string
	help      bool
}

var (
	windowsMu sync.Mutex
	windows   []*window
)

// openWindow opens a window on the repository holding dir.
func openWindow(dir string, src source) error {
	repo, err := git.Open(dir)
	if err != nil {
		return err
	}
	if src.kind == sourceCommit {
		hash, err := repo.Resolve(src.ref)
		if err != nil {
			return err
		}
		src.ref = hash
	}
	if src.kind == sourcePull {
		ref, err := resolvePull(repo, src.ref)
		if err != nil {
			return err
		}
		src.ref = ref
	}
	// A repository already open comes to the front.
	windowsMu.Lock()
	for _, w := range windows {
		if w.repo.Root == repo.Root && w.launch == src {
			windowsMu.Unlock()
			w.win.Show()
			w.win.Focus()
			return nil
		}
	}
	windowsMu.Unlock()

	w := newWindow(repo, src)
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:          windowTitle(repo.Root, src),
		Width:          1280,
		Height:         860,
		MinWidth:       720,
		MinHeight:      420,
		StateKey:       "main",
		TitleBarStyle:  mygo.TitleBarHiddenInset,
		TitleBarHeight: 52,
		Vibrancy:       mygo.VibrancySidebar,
		Content:        ui.View(w.view),
	})
	windowsMu.Lock()
	windows = append(windows, w)
	windowsMu.Unlock()
	stop := make(chan struct{})
	w.win.OnClose(func(*mygo.CloseEvent) {
		if w.settings.CopyCommentsOnClose && len(w.comments) > 0 {
			mygo.Clipboard.WriteText(w.commentsMarkdown())
		}
	})
	offSettings := cfg.OnChange(func(s Settings) { w.win.Update(func() { w.applySettings(s) }) })
	w.win.OnClosed(func() {
		close(stop)
		offSettings()
		windowsMu.Lock()
		for i, o := range windows {
			if o == w {
				windows = append(windows[:i], windows[i+1:]...)
				break
			}
		}
		windowsMu.Unlock()
	})
	w.win.OnFocus(func() { go w.checkChanges() })
	go noteRecent(repo.Root)
	w.load()
	w.loadHistory()
	w.loadUser()
	w.loadGitGraph()
	go w.watch(stop)
	w.captureIfAsked()
	return nil
}

// newWindow makes the state of a window reviewing a source of a
// repository.
func newWindow(repo *git.Repo, src source) *window {
	width, shown := state.layout()
	gitH, gitBW, gitShown := state.gitLayout()
	projects := state.projects()
	hasRoot := false
	for _, p := range projects {
		if samePath(p, repo.Root) {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		projects = append([]string{repo.Root}, projects...)
		go state.setProjects(projects)
	}

	w := &window{
		repo:               repo,
		launch:             src,
		source:             src,
		settings:           cfg.Get(),
		sidebarShown:       shown,
		sidebarWidth:       width,
		gitGraphShown:      gitShown,
		gitGraphHeight:     gitH,
		gitBranchWidth:     gitBW,
		gitTreeCollapsed:   map[string]bool{},
		projects:           projects,
		projectStates:      map[string]*projectState{},
		historyLimit:       historyPage,
		viewed:             state.viewed(viewedKey(repo.Root, src)),
		hscroll:            map[string]float32{},
		closedDirs:         map[string]bool{},
		commitTimes:        map[string]commitTime{},
		reloaded:           map[string]bool{},
		fileMatches:        map[int]bool{},
		selFile:            -1,
		selHunk:            -1,
		prs:                map[source]*pullState{},
		analyses:           map[source]*analysisState{},
	}
	w.gh, _ = findGitHubRemote(repo, github.Repo{})
	if src.kind == sourcePull {
		w.pr = w.pullFor(src)
	}
	w.loadAnalysis(src)
	w.dragWidth = w.sidebarWidth
	w.dragGitGraphHeight = w.gitGraphHeight
	w.dragGitBranchWidth = w.gitBranchWidth
	w.list.Key = func(i int) any { return w.key(&w.rows[i]) }
	w.list.Header = func(i int) bool { return w.rows[i].kind == rowHeader }
	return w
}

// canonicalPath resolves symlinks even if the leaf file/directory has been deleted.
func canonicalPath(p string) string {
	clean := filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(clean); err == nil {
		return r
	}
	dir, base := filepath.Dir(clean), filepath.Base(clean)
	if dir != clean && dir != "." && dir != "/" {
		return filepath.Join(canonicalPath(dir), base)
	}
	return clean
}

// samePath reports whether two paths refer to the same directory,
// accounting for symlinks such as macOS's /var -> /private/var.
func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	return canonicalPath(ca) == canonicalPath(cb)
}

// isProjectValid reports whether dir exists on disk and is a valid git repository.
// It performs fast stat checks on .git/HEAD without spawning any external git processes.
func isProjectValid(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	// Fast check for .git directory or submodule gitlink file
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	// Bare repo fallback (HEAD + objects)
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "objects")); err == nil {
			return true
		}
	}
	return false
}

// ensureProject adds root to projects if not already present and saves.
func (w *window) ensureProject(root string) {
	for _, p := range w.projects {
		if samePath(p, root) {
			return
		}
	}
	w.projects = append(w.projects, root)
	go state.setProjects(w.projects)
}

// addProject adds root to the window's project list and optionally switches to it.
func (w *window) addProject(root string, andSwitch bool) error {
	nextRepo, err := git.Open(root)
	if err != nil {
		return err
	}
	canonical := nextRepo.Root
	w.ensureProject(canonical)
	if andSwitch {
		return w.switchProject(canonical)
	}
	return nil
}

// addProjects adds multiple roots to the project list, ensures persistence, and switches to the last valid project.
func (w *window) addProjects(roots []string) error {
	var validRoots []string
	for _, r := range roots {
		nextRepo, err := git.Open(r)
		if err != nil {
			continue
		}
		canonical := nextRepo.Root
		w.ensureProject(canonical)
		validRoots = append(validRoots, canonical)
	}
	if len(validRoots) == 0 {
		return errors.New("no valid Git repositories found in selection")
	}
	return w.switchProject(validRoots[len(validRoots)-1])
}

// removeProject removes a project from the window.
func (w *window) removeProject(root string) {
	var nextProjects []string
	for _, p := range w.projects {
		if !samePath(p, root) {
			nextProjects = append(nextProjects, p)
		}
	}
	if len(nextProjects) == 0 {
		return
	}
	w.projects = nextProjects
	go state.setProjects(w.projects)
	for k := range w.projectStates {
		if samePath(k, root) {
			delete(w.projectStates, k)
		}
	}

	// If the current project is removed, switch to another valid one if possible.
	if w.repo != nil && samePath(w.repo.Root, root) {
		var target string
		for _, p := range nextProjects {
			if isProjectValid(p) {
				target = p
				break
			}
		}
		if target == "" {
			target = nextProjects[0]
		}
		_ = w.switchProject(target)
	} else {
		w.invalidate()
	}
}

// reorderProject moves the project at from index to to index and saves.
func (w *window) reorderProject(from, to int) {
	if from < 0 || from >= len(w.projects) || to < 0 || to >= len(w.projects) || from == to {
		return
	}
	p := w.projects[from]
	w.projects = slices.Delete(w.projects, from, from+1)
	w.projects = slices.Insert(w.projects, to, p)
	go state.setProjects(w.projects)
	w.invalidate()
}

// switchProject switches the window to review another repository directory.
func (w *window) switchProject(root string) error {
	// Guard against deleted / invalid projects.
	if !isProjectValid(root) {
		w.projectPopoverOpen = false
		return errors.New("project directory no longer exists or is not a git repository")
	}

	// 1. Resolve repository
	var nextRepo *git.Repo
	for _, s := range w.projectStates {
		if s.repo != nil && samePath(s.repo.Root, root) {
			nextRepo = s.repo
			break
		}
	}
	if nextRepo == nil {
		var err error
		nextRepo, err = git.Open(root)
		if err != nil {
			return err
		}
	}
	canonical := nextRepo.Root
	if w.repo != nil && samePath(w.repo.Root, canonical) {
		w.projectPopoverOpen = false
		return nil
	}

	// 2. Save current project's state.
	if w.repo != nil {
		if w.projectStates == nil {
			w.projectStates = map[string]*projectState{}
		}
		if w.sessions == nil {
			w.sessions = map[source]*session{}
		}
		w.sessions[w.source] = &session{comments: w.comments, viewed: w.viewed}
		w.projectStates[w.repo.Root] = &projectState{
			repo:      w.repo,
			source:    w.source,
			branch:    w.branch,
			viewed:    w.viewed,
			comments:  w.comments,
			tab:       w.tab,
			filter:    w.filter,
			sessions:  w.sessions,
			analyses:  w.analyses,
			groups:    w.groups,
			groupPref: w.groupPref,
		}
	}

	// 3. Ensure the project is in the list.
	w.ensureProject(canonical)

	// 4. Switch repository and reset diff states.
	w.repo = nextRepo
	w.signature = ""
	w.changed = false
	w.files = nil
	w.rows = nil
	w.commit = nil
	w.loadErr = nil
	w.loading = true
	w.loadedOnce = false
	w.rowsDirty = true
	w.projectPopoverOpen = false
	w.commitOpen = false

	var saved *projectState
	for k, s := range w.projectStates {
		if samePath(k, canonical) {
			saved = s
			break
		}
	}

	if saved != nil {
		w.source = saved.source
		w.branch = saved.branch
		w.viewed = saved.viewed
		w.comments = saved.comments
		w.tab = saved.tab
		w.filter = saved.filter
		w.sessions = saved.sessions
		w.analyses = saved.analyses
		w.groups = saved.groups
		w.groupPref = saved.groupPref
	} else {
		w.source = source{kind: sourceWorkingTree}
		w.branch = ""
		w.viewed = state.viewed(viewedKey(canonical, w.source))
		w.comments = nil
		w.tab = 0
		w.filter = ""
		w.sessions = map[source]*session{}
		w.analyses = map[source]*analysisState{}
		w.groups = nil
		w.groupPref = groupNone
	}

	w.gh, _ = findGitHubRemote(w.repo, github.Repo{})
	w.pullsLoaded = false
	w.pulls = nil
	w.pullsErr = nil
	w.pr = nil
	w.prs = map[source]*pullState{}
	if w.source.kind == sourcePull {
		w.pr = w.pullFor(w.source)
	}

	w.buildTree()
	w.list = ui.ListState{Key: w.list.Key, Header: w.list.Header}
	w.selFile, w.selHunk = -1, -1
	w.finding, w.query = false, ""
	w.hscroll = map[string]float32{}
	w.current = 0

	if w.win != nil {
		w.win.SetTitle(windowTitle(canonical, w.source))
	}
	go noteRecent(canonical)

	w.load()
	w.loadHistory()
	w.loadUser()
	w.loadGitGraph()
	w.invalidate()
	return nil
}

// windowTitle is "<repository>[/<source>] · Godiff".
func windowTitle(root string, src source) string {
	name := filepath.Base(root)
	switch src.kind {
	case sourceCommit:
		name += "/" + shortHash(src.ref)
	case sourceBranch:
		name += "/" + src.ref
	case sourcePull:
		_, n := src.pull()
		name += fmt.Sprintf("/#%d", n)
	}
	return name + " · Godiff"
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// applySettings takes settings changed in the file or the menus.
func (w *window) applySettings(s Settings) {
	old := w.settings
	w.settings = s
	if old.ShowWhitespace != s.ShowWhitespace {
		w.load()
	}
	if old.DiffStyle != s.DiffStyle || old.WordWrap != s.WordWrap || old.CodeFontSize != s.CodeFontSize {
		w.rowsDirty = true
	}
}

// background runs fn on a goroutine of its own, or at once in tests,
// which have no window.
func (w *window) background(fn func()) {
	if w.win == nil {
		if w.hold {
			// Tests run frames while the work waits.
			w.held = append(w.held, fn)
			return
		}
		fn()
		return
	}
	go fn()
}

// update runs fn on the main thread, then draws a frame.
func (w *window) update(fn func()) {
	if w.win == nil {
		fn()
		return
	}
	if debugFrames {
		inner := fn
		fn = func() {
			start := time.Now()
			inner()
			if d := time.Since(start); d > 2*time.Millisecond {
				log.Printf("slow update: %v", d)
			}
		}
	}
	w.win.Update(fn)
}

func (w *window) invalidate() {
	if w.win != nil {
		w.win.Invalidate()
	}
}

func (w *window) split() bool { return w.settings.DiffStyle != "unified" }

// splitFile reports whether a file shows side by side: files that are
// all new or all gone show in one column.
func (w *window) splitFile(f *fileState) bool {
	if !w.split() {
		return false
	}
	return !f.oneSided()
}

// setSource shows another source in the window: at once, with what is
// known of it, as the commit's message from the history; its changes come
// as they load.
func (w *window) setSource(src source) {
	if w.source == src {
		return
	}
	if debugFrames {
		w.switchedAt = time.Now()
		log.Printf("switch to %+v", src)
		go probeMainThread(time.Second)
	}
	w.commitOpen = false
	// The review takes the focus as the window opens only: the user, who
	// chose this source, keeps the focus where they put it, as on the
	// history.
	w.focusedOnce = true
	w.switchTo(src)
	if src.kind == sourceCommit {
		for i := range w.history {
			if w.history[i].Hash == src.ref {
				c := w.history[i]
				w.commit = &c
			}
		}
	}
	w.load()
}

// switchTo makes a source the one shown, its review in place of the
// other's.
func (w *window) switchTo(src source) {
	// Each source keeps its review: the comments, and what was viewed of a
	// commit.
	if w.sessions == nil {
		w.sessions = map[source]*session{}
	}
	w.sessions[w.source] = &session{comments: w.comments, viewed: w.viewed}
	w.comments, w.viewed = nil, nil
	if s := w.sessions[src]; s != nil {
		w.comments, w.viewed = s.comments, s.viewed
	}
	if w.viewed == nil {
		w.viewed = map[string]string{}
		if key := viewedKey(w.repo.Root, src); key != "" {
			w.viewed = state.viewed(key)
		}
	}
	w.source = src
	w.files, w.rows, w.commit, w.groups = nil, nil, nil, nil
	w.pr = nil
	if src.kind == sourcePull {
		w.pr = w.pullFor(src)
	}
	w.loadAnalysis(src)
	// The tree refers to the files by their index: it goes with them.
	w.buildTree()
	w.loadErr = nil
	w.list = ui.ListState{Key: w.list.Key, Header: w.list.Header}
	w.selFile, w.selHunk = -1, -1
	w.finding, w.query = false, ""
	w.hscroll = map[string]float32{}
	w.current = 0
	if w.win != nil {
		w.win.SetTitle(windowTitle(w.repo.Root, src))
	}
}

// preloadBudget is how long after it starts a load may wait for the
// contents of the first files, to show them colored at once rather than
// as they come.
const preloadBudget = 80 * time.Millisecond

// load reads the changes of the source, the contents of the first files
// within preloadBudget, then the contents of the others.
func (w *window) load() {
	w.gen++
	gen := w.gen
	src := w.source
	opts := git.Options{ShowWhitespace: w.settings.ShowWhitespace}
	w.loading = true
	w.loadStart = time.Now()
	started := w.loadStart
	w.loadErr = nil
	w.changed = false
	w.genA.Store(int64(gen))
	// What is known already needs no git: the commit, from the history, and
	// the branch, which showing a commit does not change.
	known := w.commit
	if known != nil && known.Hash != src.ref {
		known = nil
	}
	branch := w.branch
	needBranch := src.kind != sourceCommit || branch == ""
	remote := w.pullRemote(src)
	w.loadStatus = ""
	// Files unchanged since the last load keep their contents.
	loadedFiles := map[string]bool{}
	for _, f := range w.files {
		if f.loaded {
			loadedFiles[f.Path+"\x00"+f.Fingerprint] = true
		}
	}
	w.background(func() {
		var (
			files  []*diff.File
			commit = known
			base   string
			pulled pulledRequest
			err    error
			wg     sync.WaitGroup
		)
		if needBranch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				branch = w.repo.Branch()
			}()
		}
		sig := ""
		switch src.kind {
		case sourceWorkingTree:
			sig = w.repo.StatusSignature()
			files, err = w.repo.WorkingTree(opts)
		case sourceCommit:
			if commit == nil {
				var c git.Commit
				c, err = w.repo.CommitInfo(src.ref)
				commit = &c
			}
			if err == nil {
				files, err = w.repo.CommitDiff(*commit, opts)
			}
		case sourceBranch:
			sig = w.repo.StatusSignature()
			files, base, err = w.repo.Compare(src.ref, opts)
		case sourcePull:
			pulled, err = w.fetchPull(gen, src, remote, opts)
			files, base = pulled.files, pulled.mergeBase
		}
		wg.Wait()
		// The revisions of the old and the new side; "" is the work tree.
		var oldRev, newRev string
		switch src.kind {
		case sourceWorkingTree:
			if w.repo.HasHead() {
				oldRev = "HEAD"
			}
		case sourceCommit:
			if commit != nil && len(commit.Parents) > 0 {
				oldRev = commit.Parents[0]
			}
			newRev = src.ref
		case sourceBranch:
			oldRev = base
		case sourcePull:
			oldRev, newRev = base, pulled.head
		}
		var pre map[string]loaded
		if err == nil && !w.stale(gen) {
			var fresh []*diff.File
			for _, f := range files {
				if !loadedFiles[f.Path+"\x00"+f.Fingerprint] {
					fresh = append(fresh, f)
				}
			}
			if budget := preloadBudget - time.Since(started); budget > 0 {
				pre = w.preload(fresh, oldRev, newRev, budget)
			}
		}
		w.update(func() {
			if gen != w.gen {
				return
			}
			w.loading = false
			w.loadErr = err
			w.loadStatus = ""
			w.branch = branch
			if err == nil {
				w.commit = commit
			}
			if src.kind == sourcePull && err == nil {
				w.pr.apply(pulled)
				w.loadReviews()
			}
			w.base = base
			if sig != "" {
				w.signature = sig
			}
			w.setFiles(files)
			for _, f := range w.files {
				if l, ok := pre[f.Path]; ok && !f.loaded {
					l.file = f
					l.apply()
				}
			}
			if !w.loadedOnce && len(files) == 0 && src.kind != sourceCommit && src.kind != sourcePull {
				// Nothing to review: the history shows instead, and takes
				// the keys in place of the review.
				w.tab = 1
				w.focusHistory = w.sidebarShown
				w.focusedOnce = true
			}
			w.loadedOnce = true
			if err == nil {
				var rest []*fileState
				for _, f := range w.files {
					if !f.loaded {
						rest = append(rest, f)
					}
				}
				if len(rest) > 0 {
					w.background(func() { w.loadContents(gen, oldRev, newRev, rest) })
				}
			}
		})
	})
}

// preload reads and colors the contents of files, in order, until
// budget runs out: the files it did not finish load later, as they come.
func (w *window) preload(files []*diff.File, oldRev, newRev string, budget time.Duration) map[string]loaded {
	if len(files) == 0 {
		return nil
	}
	contents, err := w.repo.NewContents()
	if err != nil {
		return nil
	}
	defer boostGC()()
	results := make(chan loaded)
	var stop atomic.Bool
	go func() {
		defer close(results)
		defer contents.Close()
		for _, f := range files {
			if stop.Load() {
				return
			}
			l := w.read(contents, f, oldRev, newRev)
			l.finish()
			select {
			case results <- l:
			case <-time.After(budget):
				return // nobody waits anymore
			}
		}
	}()
	out := map[string]loaded{}
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	for {
		select {
		case l, ok := <-results:
			if !ok {
				return out
			}
			out[l.path] = l
		case <-deadline.C:
			stop.Store(true)
			return out
		}
	}
}

// setFiles shows newly read files, keeping what the user did to those
// that did not change.
func (w *window) setFiles(files []*diff.File) {
	prev := map[string]*fileState{}
	for _, f := range w.files {
		prev[f.Path] = f
	}
	// A refresh marks the files that changed since, and opens them.
	refresh := len(prev) > 0
	w.reloaded = map[string]bool{}
	next := make([]*fileState, 0, len(files))
	for _, f := range files {
		fs := &fileState{File: f}
		switch p := prev[f.Path]; {
		case p != nil && p.Fingerprint == f.Fingerprint:
			fs.collapsed = p.collapsed
			fs.expanded = p.expanded
			// The contents stay until they load again.
			fs.oldLines, fs.newLines, fs.oldHL, fs.newHL = p.oldLines, p.newLines, p.oldHL, p.newHL
			fs.oldImage, fs.newImage, fs.oldSize, fs.newSize = p.oldImage, p.newImage, p.oldSize, p.newSize
			fs.loaded = p.loaded
		case refresh:
			w.reloaded[f.Path] = true
			fs.collapsed = f.Generated || f.Directory
		default:
			fs.collapsed = w.isViewed(fs) || f.Generated || f.Directory
		}
		next = append(next, fs)
	}
	w.files = next
	w.regroup()
	w.pruneComments()
}

// isViewed reports whether the user marked the file viewed, as it is now.
func (w *window) isViewed(f *fileState) bool {
	return f.Fingerprint != "" && w.viewed[f.Path] == f.Fingerprint
}

func (w *window) setViewed(f *fileState, viewed bool) {
	fp := ""
	if viewed {
		fp = f.Fingerprint
		w.viewed[f.Path] = fp
	} else {
		delete(w.viewed, f.Path)
	}
	// Only the work tree's and pull requests' files are remembered:
	// commits do not change.
	if key := viewedKey(w.repo.Root, w.source); key != "" {
		go state.setViewed(key, f.Path, fp)
	}
	f.collapsed = viewed
	w.rowsDirty = true
}

// maxContent is the size of the files whose contents are loaded.
const maxContent = 2 << 20

// loaded is a file's contents, read and tokenized, or decoded for
// pictures.
type loaded struct {
	file               *fileState // where they go, once known
	oldLines, newLines []string
	oldHL, newHL       [][]highlight.Seg
	oldData, newData   []byte // of pictures
	oldImage, newImage *ui.Bitmap
	path, oldPath      string
}

// read reads both sides of a file: lines of text, or the data of a
// picture.
func (w *window) read(contents *git.Contents, f *diff.File, oldRev, newRev string) loaded {
	l := loaded{path: f.Path, oldPath: f.OldPath}
	raw := func(rev, path string, limit int) []byte {
		var data []byte
		if rev == "" {
			data = w.repo.ReadWorkTree(path)
		} else {
			data, _ = contents.Read(rev, path)
		}
		if len(data) > limit {
			return nil
		}
		return data
	}
	hasOld := oldRev != "" && f.Status != diff.Added && f.Status != diff.Untracked
	hasNew := f.Status != diff.Deleted
	if f.Binary && isImage(f.Path) {
		if hasOld {
			l.oldData = raw(oldRev, f.OldPath, maxImage)
		}
		if hasNew {
			l.newData = raw(newRev, f.Path, maxImage)
		}
		return l
	}
	if f.Binary || f.Directory || f.TooLarge {
		return l
	}
	text := func(data []byte) []string {
		if data == nil || diff.IsBinary(data) {
			return nil
		}
		lines := diff.SplitLines(string(data))
		if lines == nil {
			lines = []string{}
		}
		return lines
	}
	if hasOld {
		l.oldLines = text(raw(oldRev, f.OldPath, maxContent))
	}
	if hasNew {
		l.newLines = text(raw(newRev, f.Path, maxContent))
	}
	return l
}

// finish colors the lines, and decodes the pictures.
func (l *loaded) finish() {
	l.oldHL = highlight.Lines(l.oldPath, l.oldLines)
	l.newHL = highlight.Lines(l.path, l.newLines)
	if l.oldData != nil {
		l.oldImage, _ = ui.DecodeBitmap(l.oldData)
	}
	if l.newData != nil {
		l.newImage, _ = ui.DecodeBitmap(l.newData)
	}
}

// apply gives the contents to their file, on the main thread.
func (l *loaded) apply() {
	f := l.file
	f.oldLines, f.newLines = l.oldLines, l.newLines
	f.oldHL, f.newHL = l.oldHL, l.newHL
	f.oldImage, f.newImage = l.oldImage, l.newImage
	f.oldSize, f.newSize = len(l.oldData), len(l.newData)
	f.loaded = true
	f.metricsDone = false
	f.spans = nil
}

// maxImage is the size of the pictures shown.
const maxImage = 32 << 20

// isImage reports whether a path is of a picture the app shows.
func isImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".ico":
		return true
	}
	return false
}

// loadContents reads both sides of the files, for their colors and their
// unchanged lines, and shows them as they come.
func (w *window) loadContents(gen int, oldRev, newRev string, files []*fileState) {
	defer boostGC()()
	contents, err := w.repo.NewContents()
	if err != nil {
		return
	}
	defer contents.Close()

	jobs := make(chan loaded)
	results := make(chan loaded)
	var wg sync.WaitGroup
	// A few workers: more would take the CPU, and the collector's pauses,
	// from the main thread, which draws the window.
	for range highlightWorkers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				job.finish()
				results <- job
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, f := range files {
			if w.stale(gen) {
				return
			}
			job := w.read(contents, f.File, oldRev, newRev)
			job.file = f
			jobs <- job
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var batch []loaded
	flush := func() {
		if len(batch) == 0 {
			return
		}
		done := batch
		batch = nil
		w.update(func() {
			if gen != w.gen {
				return
			}
			for i := range done {
				done[i].apply()
			}
			w.rowsDirty = true
		})
	}
	tick := time.NewTicker(60 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case l, ok := <-results:
			if !ok {
				flush()
				return
			}
			batch = append(batch, l)
		case <-tick.C:
			flush()
		}
	}
}

// boostGC lets the heap grow further between collections while files load:
// Chroma's lexers make much garbage, and every collection has the main
// thread, which allocates as it draws, help with it. The function it
// returns ends the boost.
func boostGC() func() {
	gcBoost.Lock()
	if gcBoost.n == 0 {
		gcBoost.old = debug.SetGCPercent(400)
	}
	gcBoost.n++
	gcBoost.Unlock()
	return func() {
		gcBoost.Lock()
		gcBoost.n--
		if gcBoost.n == 0 {
			debug.SetGCPercent(gcBoost.old)
		}
		gcBoost.Unlock()
	}
}

var gcBoost struct {
	sync.Mutex
	n, old int
}

// highlightWorkers is how many files are highlighted at once.
func highlightWorkers() int { return min(max(runtime.NumCPU()/4, 1), 2) }

func (w *window) stale(gen int) bool { return w.genA.Load() != int64(gen) }

// loadUser reads the name of the git user, which comments show, off the
// main thread: running git takes a while.
func (w *window) loadUser() {
	w.background(func() {
		name := strings.TrimSpace(w.repo.ConfigValue("user.name"))
		if name == "" {
			name = strings.TrimSpace(w.repo.ConfigValue("user.email"))
		}
		w.update(func() { w.user = name })
	})
}

// loadHistory reads the commits of the History tab.
func (w *window) loadHistory() {
	limit := w.historyLimit
	w.historyLoading = true
	w.background(func() {
		commits, err := w.repo.Log(0, limit)
		if err != nil {
			commits = nil
		}
		w.update(func() {
			w.history = commits
			w.historyMore = len(commits) >= limit
			w.historyLoading = false
		})
	})
}

// watch notices changes of the work tree, polling as codiff does: often
// while the window has the focus, rarely while it is in the background.
func (w *window) watch(stop chan struct{}) {
	for {
		delay := 10 * time.Second
		if w.win.IsFocused() {
			delay = 2500 * time.Millisecond
		} else if !w.win.IsVisible() || w.win.IsMinimized() {
			delay = 30 * time.Second
		}
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
		w.checkChanges()
	}
}

// checkChanges compares the work tree with what the window shows.
func (w *window) checkChanges() {
	var src source
	var sig string
	var busy bool
	var repo *git.Repo
	mygo.RunOnMain(func() {
		src, sig, busy = w.source, w.signature, w.loading || w.changed
		repo = w.repo
	})
	if repo == nil || busy || sig == "" || src.kind == sourceCommit {
		return
	}
	if src.kind == sourcePull {
		w.checkPull(src)
		return
	}
	if now := repo.StatusSignature(); now != sig {
		w.win.Update(func() {
			if w.repo == repo && w.source == src && !w.loading {
				w.changed = true
			}
		})
	}
}

// refresh loads the source again, and the history, and the pull
// requests once listed.
func (w *window) refresh() {
	w.load()
	w.loadHistory()
	if w.pullsLoaded {
		w.loadPulls()
	}
}

// openInEditor opens a file of the repository in the user's editor, at a
// line.
func (w *window) openInEditor(path string, line int) {
	abs := filepath.Join(w.repo.Root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		abs = w.repo.Root
		line = 0
	}
	go func() {
		if err := openEditor(w.settings.EditorCommand, w.repo.Root, abs, line); err != nil {
			mygo.Dialog.Error("Could not open the file", err.Error())
		}
	}()
}

// abbreviateHome writes the home directory as ~.
func abbreviateHome(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// shortPath abbreviates the directories of a path to their first letter,
// as fish does: ~/P/fate.
func shortPath(p string) string {
	p = abbreviateHome(p)
	parts := strings.Split(p, "/")
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" || part == "~" {
			continue
		}
		n := 1
		if strings.HasPrefix(part, ".") && len(part) > 1 {
			n = 2
		}
		r := []rune(part)
		parts[i] = string(r[:min(n, len(r))])
	}
	return strings.Join(parts, "/")
}

// errorText is the text of an error for the window.
func errorText(err error) string {
	var ge *git.Error
	if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) != "" {
		return strings.TrimSpace(ge.Stderr)
	}
	return fmt.Sprint(err)
}

// probeMainThread logs how long the main thread kept a function waiting,
// at worst, over a while: how long the window could not respond.
func probeMainThread(d time.Duration) {
	var worst, total time.Duration
	var slow, n int
	for end := time.Now().Add(d); time.Now().Before(end); n++ {
		start := time.Now()
		mygo.RunOnMain(func() {})
		wait := time.Since(start)
		total += wait
		worst = max(worst, wait)
		if wait > 16*time.Millisecond {
			slow++
		}
		time.Sleep(2 * time.Millisecond)
	}
	log.Printf("main thread: worst wait %v, average %v, %d of %d waits over 16ms", worst, total/time.Duration(max(n, 1)), slow, n)
}

// watchMainThread logs, for GODIFF_DEBUG, whenever the main thread keeps
// a function waiting over 30ms: when the window cannot respond.
func watchMainThread() {
	limit := 30 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("GODIFF_DEBUG_BLOCK_MS")); err == nil {
		limit = time.Duration(ms) * time.Millisecond
	}
	var gc debug.GCStats
	for {
		start := time.Now()
		mygo.RunOnMain(func() {})
		if wait := time.Since(start); wait > limit {
			n := gc.NumGC
			debug.ReadGCStats(&gc)
			pause := time.Duration(0)
			if len(gc.Pause) > 0 {
				pause = gc.Pause[0]
			}
			log.Printf("main thread blocked %v (GC cycles since last: %d, last pause %v)", wait, gc.NumGC-n, pause)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
