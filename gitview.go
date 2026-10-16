package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

const (
	gitPanelMinHeight  = 160
	gitPanelMaxHeight  = 650
	branchTreeMinWidth = 140
	branchTreeMaxWidth = 380
)

// toggleGitGraph toggles the visibility of the top git graph and branch tree panel.
func (w *window) toggleGitGraph() {
	w.gitGraphShown = !w.gitGraphShown
	if w.gitGraphShown && (w.gitGraphLayout == nil || len(w.gitAllCommits) == 0) {
		w.loadGitGraph()
	}
	go state.setGitLayout(w.gitGraphHeight, w.gitBranchWidth, w.gitGraphShown)
}

// loadGitGraph loads all branches, tags, and commits across the repository in the background.
func (w *window) loadGitGraph() {
	if w.repo == nil || w.gitGraphLoading {
		return
	}
	w.gitGraphLoading = true
	repo := w.repo
	curBranch := w.branch

	go func() {
		refs, _ := repo.Refs()
		commits, _ := repo.LogAll(0, 1000)

		// Check if worktree has uncommitted changes
		hasUncommitted := false
		if w.source.kind == sourceWorkingTree && len(w.files) > 0 {
			hasUncommitted = true
		} else {
			hasUncommitted = repo.StatusSignature() != ""
		}

		layout := BuildGraph(commits, curBranch, hasUncommitted)

		if w.win != nil {
			w.win.Update(func() {
				w.gitRefs = refs
				w.gitAllCommits = commits
				w.gitGraphLayout = layout
				w.gitGraphLoading = false
			})
		}
	}()
}

// gitGraphPanel renders the top panel with the branch tree on the left and the commit graph on the right.
func (w *window) gitGraphPanel(c *ui.Context, pal *palette) {
	h := w.gitGraphHeight
	if h <= 0 {
		h = 280
	}

	ui.Row(c).Height(h).FillWidth().Background(pal.appBg).BorderWidth(0, 0, 1, 0).
		BorderColor(pal.cardBorder).Children(func() {
		// Left: Branch Tree view
		w.branchTreeView(c, pal)

		// Divider between branch tree and commit graph
		w.gitBranchResizer(c, pal)

		// Right: Commit Graph view
		w.commitGraphView(c, pal)
	})
}

// gitBranchResizer is the vertical drag handle between branch tree and commit graph.
func (w *window) gitBranchResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Width(1).FillHeight().Background(pal.cardBorder).Shrink(0).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(9).Cursor(ui.CursorResizeEW)
		if dx, _, held := handle.Dragged(); held {
			w.dragGitBranchWidth += dx
			w.gitBranchWidth = min(max(w.dragGitBranchWidth, branchTreeMinWidth), branchTreeMaxWidth)
		} else {
			if w.dragGitBranchWidth != 0 && w.dragGitBranchWidth != w.gitBranchWidth {
				go state.setGitLayout(w.gitGraphHeight, w.gitBranchWidth, w.gitGraphShown)
			}
			w.dragGitBranchWidth = w.gitBranchWidth
		}
	})
}

// gitPanelResizer is the horizontal drag handle between the top git panel and bottom diff area.
func (w *window) gitPanelResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Height(1).FillWidth().Background(pal.cardBorder).Shrink(0).Children(func() {
		handle := ui.Box(c).Absolute().Left(0).Right(0).Top(-4).Height(9).Cursor(ui.CursorResizeNS)
		if _, dy, held := handle.Dragged(); held {
			w.dragGitGraphHeight += dy
			w.gitGraphHeight = min(max(w.dragGitGraphHeight, gitPanelMinHeight), gitPanelMaxHeight)
		} else {
			if w.dragGitGraphHeight != 0 && w.dragGitGraphHeight != w.gitGraphHeight {
				go state.setGitLayout(w.gitGraphHeight, w.gitBranchWidth, w.gitGraphShown)
			}
			w.dragGitGraphHeight = w.gitGraphHeight
		}
	})
}

type branchRowKind int

const (
	rowAllCommits branchRowKind = iota
	rowSectionHeader
	rowDir
	rowLeaf
)

type branchTreeRow struct {
	key       string
	kind      branchRowKind
	name      string
	ref       *git.Ref
	depth     int
	collapsed bool
	isCurrent bool
	icon      *ui.SVG
}

// visibleBranchRows flattens the hierarchical branch trees into a linear slice for ui.List.
func (w *window) visibleBranchRows() []branchTreeRow {
	var rows []branchTreeRow

	// 1. All Commits top row
	rows = append(rows, branchTreeRow{
		key:  "all_commits",
		kind: rowAllCommits,
		name: "All commits",
		icon: iconCommit,
	})

	if w.gitTreeCollapsed == nil {
		w.gitTreeCollapsed = map[string]bool{}
	}

	branches, remotes, tags := BuildBranchTrees(w.gitRefs, w.branch, w.gitFilter)

	// Helper to append a section
	appendSection := func(secKey, secTitle string, nodes []*BranchTreeNode, secIcon *ui.SVG) {
		if len(nodes) == 0 {
			return
		}
		collapsed := w.gitTreeCollapsed[secKey]
		rows = append(rows, branchTreeRow{
			key:       secKey,
			kind:      rowSectionHeader,
			name:      secTitle,
			collapsed: collapsed,
			icon:      secIcon,
		})
		if collapsed {
			return
		}

		var appendNode func(n *BranchTreeNode, depth int)
		appendNode = func(n *BranchTreeNode, depth int) {
			if !n.IsLeaf {
				dirKey := secKey + ":dir:" + n.FullPath
				dirCollapsed := w.gitTreeCollapsed[dirKey]
				rows = append(rows, branchTreeRow{
					key:       dirKey,
					kind:      rowDir,
					name:      n.Name,
					depth:     depth,
					collapsed: dirCollapsed,
				})
				if !dirCollapsed {
					for _, child := range n.Children {
						appendNode(child, depth+1)
					}
				}
			} else {
				rows = append(rows, branchTreeRow{
					key:       secKey + ":leaf:" + n.FullPath,
					kind:      rowLeaf,
					name:      n.Name,
					ref:       n.Ref,
					depth:     depth,
					isCurrent: n.IsCurrent,
				})
			}
		}

		for _, n := range nodes {
			appendNode(n, 1)
		}
	}

	appendSection("branches", "Branches", branches, iconBranch)
	appendSection("remotes", "Remote branches", remotes, iconPull)
	appendSection("tags", "Tags", tags, iconSparkle)

	return rows
}

// branchTreeView renders the left sidebar of the top git panel with scrollable list and pinned filter bar.
func (w *window) branchTreeView(c *ui.Context, pal *palette) {
	t := c.Theme()
	bw := w.gitBranchWidth
	if bw <= 0 {
		bw = 200
	}

	rows := w.visibleBranchRows()

	w.gitBranchTreeList.Key = func(i int) any { return rows[i].key }
	w.gitBranchTreeList.Label = func(i int) string { return rows[i].name }

	ui.Column(c).Width(bw).FillHeight().Background(pal.headerBg.Alpha(0.4)).Shrink(0).Children(func() {
		// 1. Scrollable branch tree list (using ui.List for virtual scrolling)
		ui.List(c, &w.gitBranchTreeList, len(rows), func(i int) {
			r := rows[i]
			indent := float32(r.depth * 10)

			switch r.kind {
			case rowAllCommits:
				row := ui.Row(c.Key(r.key)).Height(26).Padding(0, 8).Gap(6).Radius(5).Cursor(ui.CursorPointer).
					AlignItems(ui.Center).FillWidth()
				if row.Hovered() {
					row.Background(ui.RGBA(127, 127, 127, 0.1))
				}
				if row.Clicked() {
					w.gitFilter = ""
					w.gitSelectedHash = ""
					if w.gitGraphLayout != nil && len(w.gitGraphLayout.Rows) > 0 {
						w.gitGraphList.ScrollIntoView(0)
					}
					w.loadGitGraph()
				}
				row.Children(func() {
					ui.Icon(c, r.icon).FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, r.name).FontSize(12).FontWeight(600).Grow(1)
					if len(w.gitAllCommits) > 0 {
						ui.Textf(c, "%d", len(w.gitAllCommits)).FontSize(10).FontWeight(600).
							TextColor(t.TextMuted).Padding(1, 5).Radius(4).Background(pal.pill)
					}
				})

			case rowSectionHeader:
				header := ui.Row(c.Key(r.key)).Height(24).Padding(0, 6).Gap(4).Radius(4).Cursor(ui.CursorPointer).
					AlignItems(ui.Center).FillWidth()
				if header.Hovered() {
					header.Background(ui.RGBA(127, 127, 127, 0.08))
				}
				if header.Clicked() {
					w.gitTreeCollapsed[r.key] = !r.collapsed
				}
				header.Children(func() {
					chev := iconChevronDown
					if r.collapsed {
						chev = iconChevronRight
					}
					ui.Icon(c, chev).FontSize(11).TextColor(t.TextMuted)
					if r.icon != nil {
						ui.Icon(c, r.icon).FontSize(11).TextColor(t.TextMuted)
					}
					ui.Text(c, r.name).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Grow(1)
				})

			case rowDir:
				dirRow := ui.Row(c.Key(r.key)).Height(22).Padding(0, 4, 0, indent+4).Gap(4).Radius(4).Cursor(ui.CursorPointer).
					AlignItems(ui.Center).FillWidth()
				if dirRow.Hovered() {
					dirRow.Background(ui.RGBA(127, 127, 127, 0.08))
				}
				if dirRow.Clicked() {
					w.gitTreeCollapsed[r.key] = !r.collapsed
				}
				dirRow.Children(func() {
					chev := iconChevronDown
					if r.collapsed {
						chev = iconChevronRight
					}
					ui.Icon(c, chev).FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, r.name).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine()
				})

			case rowLeaf:
				leafRow := ui.Row(c.Key(r.key)).Height(24).Padding(0, 6, 0, indent+8).Gap(6).Radius(5).Cursor(ui.CursorPointer).
					AlignItems(ui.Center).FillWidth()

				isCurrent := r.isCurrent
				isSelected := r.ref != nil && r.ref.Hash != "" && (w.gitSelectedHash == r.ref.Hash || (w.source.kind == sourceCommit && w.source.ref == r.ref.Hash))

				if isSelected {
					leafRow.Background(t.Accent.Alpha(0.18))
				} else if isCurrent {
					leafRow.Background(t.Accent.Alpha(0.08))
				} else if leafRow.Hovered() {
					leafRow.Background(ui.RGBA(127, 127, 127, 0.08))
				}

				if leafRow.Clicked() {
					if r.ref != nil && r.ref.Hash != "" {
						targetHash := r.ref.Hash
						w.gitSelectedHash = targetHash
						w.commitOpen = false

						// Jump right-side commit graph to this branch's commit position
						if w.gitGraphLayout != nil {
							for idx, gr := range w.gitGraphLayout.Rows {
								if !gr.IsUncommitted && (gr.Commit.Hash == targetHash || strings.HasPrefix(gr.Commit.Hash, targetHash)) {
									w.gitGraphList.ScrollIntoView(idx)
									break
								}
							}
						}

						// Switch diff view below to this commit
						w.setSource(source{kind: sourceCommit, ref: targetHash})
					}
				}

				leafRow.Children(func() {
					nameText := ui.Text(c, r.name).FontSize(11).SingleLine().Grow(1).MinWidth(0)
					if isCurrent {
						nameText.FontWeight(700).TextColor(t.Accent)
						ui.Icon(c, iconCheck).FontSize(11).TextColor(t.Accent).Shrink(0)
					} else if isSelected {
						nameText.FontWeight(600).TextColor(t.Text)
					} else {
						nameText.TextColor(t.Text)
					}
				})
			}
		}).Grow(1).FillWidth().Padding(4, 4)

		// 2. Pinned Bottom Filter Bar (guaranteed not covered or overlapping)
		ui.Row(c).Height(34).Shrink(0).Padding(4, 8).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).
			Background(pal.headerBg.Alpha(0.6)).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, iconSearch).FontSize(12).TextColor(t.TextMuted)
			field := ui.TextInputBase(c, &w.gitFilter).Placeholder("Filter branches…").Label("Filter branches").
				FontSize(11).Grow(1).MinWidth(0)
			if field.FocusWithin() {
				w.typing = true
			}
			if w.gitFilter != "" {
				if iconButton(c, iconClose, "Clear filter").Size(16, 16).Clicked() {
					w.gitFilter = ""
				}
			}
		})
	})
}

// commitGraphView renders the commit list with visual DAG lanes, badges, subject, author and time.
func (w *window) commitGraphView(c *ui.Context, pal *palette) {
	t := c.Theme()

	if w.gitGraphLayout == nil || len(w.gitGraphLayout.Rows) == 0 {
		ui.Column(c).Grow(1).FillHeight().Center().Children(func() {
			if w.gitGraphLoading {
				ui.Spinner(c).Size(20, 20)
				ui.Text(c, "Loading branch graph…").FontSize(12).TextColor(t.TextMuted).Margin(8, 0, 0, 0)
			} else {
				ui.Text(c, "No commits found").FontSize(12).TextColor(t.TextMuted)
			}
		})
		return
	}

	rows := w.gitGraphLayout.Rows
	const rowHeight = float32(28)
	graphW := w.gitGraphLayout.Width
	if graphW < 24 {
		graphW = 24
	}

	ui.Column(c).Grow(1).FillHeight().Children(func() {
		// 1. Table Header Row (aligned with example: 图表 | 描述 | 提交 | 作者 | 日期)
		ui.Row(c).Height(24).FillWidth().Padding(0, 10, 0, 4).AlignItems(ui.Center).Gap(6).
			Background(pal.headerBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			// Header: 图表
			ui.Row(c).Width(graphW).Height(24).AlignItems(ui.Center).Shrink(0).
				BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, "图表").FontSize(11).FontWeight(500).TextColor(pal.muted)
			})
			// Header: 描述
			ui.Row(c).Grow(1).MinWidth(0).Height(24).AlignItems(ui.Center).
				BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, "描述").FontSize(11).FontWeight(500).TextColor(pal.muted)
			})
			// Header: 提交
			ui.Row(c).Width(65).Height(24).AlignItems(ui.Center).Shrink(0).
				BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, "提交").FontSize(11).FontWeight(500).TextColor(pal.muted)
			})
			// Header: 作者
			ui.Row(c).Width(85).Height(24).AlignItems(ui.Center).Shrink(0).
				BorderWidth(0, 1, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, "作者").FontSize(11).FontWeight(500).TextColor(pal.muted)
			})
			// Header: 日期
			ui.Row(c).Width(105).Height(24).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, "日期").FontSize(11).FontWeight(500).TextColor(pal.muted)
			})
		})

		// 2. Commit List with Virtual Scrolling
		w.gitGraphList.Key = func(i int) any {
			if rows[i].IsUncommitted {
				return "uncommitted"
			}
			return rows[i].Commit.Hash
		}

		ui.List(c, &w.gitGraphList, len(rows), func(i int) {
			r := rows[i]
			isCurSource := false
			if r.IsUncommitted {
				isCurSource = (w.source.kind == sourceWorkingTree)
			} else {
				isCurSource = (w.source.kind == sourceCommit && (w.source.ref == r.Commit.Hash || strings.HasPrefix(r.Commit.Hash, w.source.ref)))
			}

			item := ui.Row(c.Key(fmt.Sprintf("grow:%d", i))).Height(rowHeight).Padding(0, 10, 0, 4).
				AlignItems(ui.Center).Gap(6).FillWidth().Cursor(ui.CursorPointer)

			if isCurSource {
				item.Background(t.Accent.Alpha(0.12))
			} else if item.Hovered() {
				item.Background(ui.RGBA(127, 127, 127, 0.08))
			}

			if item.Clicked() {
				w.commitOpen = false
				if r.IsUncommitted {
					w.gitSelectedHash = ""
					w.setSource(source{kind: sourceWorkingTree})
				} else {
					w.gitSelectedHash = r.Commit.Hash
					w.setSource(source{kind: sourceCommit, ref: r.Commit.Hash})
				}
			}

			item.Children(func() {
				// 1. Commit Graph SVG: unified compact width and ui.Image to preserve authentic vibrant colors!
				ui.Box(c).Width(graphW).Height(rowHeight).Shrink(0).Children(func() {
					if r.SVG != nil {
						ui.Image(c, r.SVG).Size(graphW, rowHeight)
					}
				})

				// 2. Ref Badges (e.g. main, origin/main, v1.0)
				for _, b := range r.Badges {
					badge := ui.Row(c).Height(18).Padding(0, 5).Gap(3).Radius(4).Background(b.BgColor).
						Border(1, b.Color.Alpha(0.35)).AlignItems(ui.Center).Shrink(0)
					badge.Children(func() {
						switch b.Kind {
						case BadgeTag:
							ui.Icon(c, iconSparkle).FontSize(10).TextColor(b.Color)
						default:
							ui.Icon(c, iconBranch).FontSize(10).TextColor(b.Color)
						}
						ui.Text(c, b.Name).FontSize(10).FontWeight(700).TextColor(b.Color)
					})
				}

				// 3. Commit Subject / Message ("描述" 列)
				if r.IsUncommitted {
					ui.Text(c, "Uncommitted changes").FontSize(12).FontWeight(700).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
				} else {
					subjText := ui.Text(c, r.Commit.Subject).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					if isCurSource {
						subjText.FontWeight(600).TextColor(t.Text)
					} else {
						subjText.TextColor(t.Text)
					}
				}

				// 4. Commit Short Hash ("提交" 列)
				hashText := r.Commit.Short
				if r.IsUncommitted {
					hashText = "*"
				}
				ui.Text(c, hashText).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).Shrink(0).Width(65)

				// 5. Author Name ("作者" 列)
				author := r.Commit.Author
				if r.IsUncommitted {
					author = "*"
				}
				ui.Text(c, author).FontSize(11).TextColor(t.TextMuted).Shrink(0).Width(85).SingleLine()

				// 6. Date / Time ("日期" 列)
				dateStr := ""
				if r.IsUncommitted {
					dateStr = formatCommitDate(time.Now())
				} else if !r.Commit.Time.IsZero() {
					dateStr = formatCommitDate(r.Commit.Time)
				}
				ui.Text(c, dateStr).FontSize(11).TextColor(t.TextMuted).Shrink(0).Width(105)
			})
		}).Grow(1).FillHeight()
	})
}

// formatCommitDate formats commit timestamp into human-readable style (今天 15:04 / 昨天 15:04 / 2006-01-02 15:04).
func formatCommitDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	itemDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	diffDays := int(today.Sub(itemDay).Hours() / 24)

	if diffDays == 0 {
		return "今天 " + t.Format("15:04")
	} else if diffDays == 1 {
		return "昨天 " + t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}
