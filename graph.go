package main

import (
	"fmt"
	"strings"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// GraphColors defines the rich palette of colors for commit graph lanes.
var graphColors = []ui.Color{
	ui.Hex("#d19a66"), // Warm Orange (often main branch)
	ui.Hex("#61afef"), // Vibrant Blue
	ui.Hex("#98c379"), // Soft Green
	ui.Hex("#c678dd"), // Purple
	ui.Hex("#e5c07b"), // Gold / Yellow
	ui.Hex("#56b6c2"), // Cyan
	ui.Hex("#e06c75"), // Coral Red
	ui.Hex("#e06cbb"), // Pink / Magenta
}

var graphHexColors = []string{
	"#d19a66",
	"#61afef",
	"#98c379",
	"#c678dd",
	"#e5c07b",
	"#56b6c2",
	"#e06c75",
	"#e06cbb",
}

func laneColor(lane int) ui.Color {
	if lane < 0 {
		return graphColors[0]
	}
	return graphColors[lane%len(graphColors)]
}

func laneHex(lane int) string {
	if lane < 0 {
		return graphHexColors[0]
	}
	return graphHexColors[lane%len(graphHexColors)]
}

// BadgeKind is the kind of reference badge.
type BadgeKind int

const (
	BadgeHead BadgeKind = iota
	BadgeLocalBranch
	BadgeRemoteBranch
	BadgeTag
)

// RefBadge represents a branch/tag badge to render before commit subject.
type RefBadge struct {
	Name    string
	Kind    BadgeKind
	Color   ui.Color
	BgColor ui.Color
}

// NodeStyle denotes the visual kind of a commit node.
type NodeStyle int

const (
	NodeStyleRegular NodeStyle = iota // Single-parent commit (hollow colored circle)
	NodeStyleMerge                    // Merge commit (solid larger node)
	NodeStyleFork                     // Fork commit (branch origin node)
	NodeStyleHead                     // HEAD / Checked-out commit (concentric white-centered halo node)
	NodeStyleUncommitted              // Working tree changes (dashed ring node)
)

// LineKind defines the connection type of a segment in a row.
type LineKind int

const (
	LineThrough LineKind = iota // Continuous pass-through vertical line
	LineIn                      // Line entering node from top (straight or curve from parent)
	LineOut                     // Line exiting node to bottom (straight or curve to parent)
)

// VisualLine is an edge to draw within a single 28px row.
type VisualLine struct {
	Kind    LineKind
	FromCol int
	ToCol   int
	Color   string
}

// GraphRow represents one row in the commit graph list.
type GraphRow struct {
	Commit        git.Commit
	IsUncommitted bool
	IsHead        bool
	NodeKind      NodeStyle
	Col           int
	Color         ui.Color
	HexColor      string
	Lines         []VisualLine
	Badges        []RefBadge
	SVG           *ui.SVG
}

// GraphLayout holds the precomputed graph rows and uniform compact dimensions.
type GraphLayout struct {
	Rows    []GraphRow
	MaxCols int
	Width   float32
}

const (
	graphRowHeight = float32(28)
	graphColStep   = float32(14)
)

// BuildGraph constructs the topological graph columns, smooth curves, and node styles.
func BuildGraph(commits []git.Commit, currentBranch string, hasUncommitted bool) *GraphLayout {
	layout := &GraphLayout{
		Rows: make([]GraphRow, 0, len(commits)+1),
	}

	// activeLanes maps column index to the expected commit hash
	type laneInfo struct {
		hash  string
		color int
	}
	activeLanes := make([]laneInfo, 0, 16)
	maxObservedCols := 1

	// Child count per commit hash to identify Fork points
	childCounts := make(map[string]int, len(commits))
	for _, c := range commits {
		for _, p := range c.Parents {
			childCounts[p]++
		}
	}

	// 1. Uncommitted changes row if present
	if hasUncommitted {
		headHash := ""
		if len(commits) > 0 {
			headHash = commits[0].Hash
		}
		activeLanes = append(activeLanes, laneInfo{hash: headHash, color: 0})
		uColor := ui.Hex("#888888")
		uHex := "#888888"
		uRow := GraphRow{
			IsUncommitted: true,
			NodeKind:      NodeStyleUncommitted,
			Col:           0,
			Color:         uColor,
			HexColor:      uHex,
			Lines: []VisualLine{
				{
					Kind:    LineOut,
					FromCol: 0,
					ToCol:   0,
					Color:   uHex,
				},
			},
			Badges: nil,
		}
		layout.Rows = append(layout.Rows, uRow)
	}

	// 2. Process each commit in topological order
	for _, c := range commits {
		hash := c.Hash

		// A. Find all active lanes waiting for this commit
		matchingCols := make([]int, 0, 4)
		for i, l := range activeLanes {
			if l.hash == hash {
				matchingCols = append(matchingCols, i)
			}
		}

		var nodeCol int
		var nodeColorIdx int

		if len(matchingCols) > 0 {
			// Primary node column is the leftmost matching lane
			nodeCol = matchingCols[0]
			nodeColorIdx = activeLanes[nodeCol].color
		} else {
			// Not waiting in active lanes: claim leftmost free slot or append
			freeCol := -1
			for i, l := range activeLanes {
				if l.hash == "" {
					freeCol = i
					break
				}
			}
			if freeCol != -1 {
				nodeCol = freeCol
			} else {
				nodeCol = len(activeLanes)
				activeLanes = append(activeLanes, laneInfo{})
			}
			nodeColorIdx = nodeCol
			activeLanes[nodeCol] = laneInfo{hash: hash, color: nodeColorIdx}
		}

		// Snapshot lanes at top of row
		topLanes := make([]laneInfo, len(activeLanes))
		copy(topLanes, activeLanes)

		// B. Determine node style (Merge, Fork, Head, Regular)
		badges, isHead := parseRefBadges(c.Refs, currentBranch)
		nodeKind := NodeStyleRegular
		if isHead {
			nodeKind = NodeStyleHead
		} else if len(c.Parents) > 1 {
			nodeKind = NodeStyleMerge
		} else if childCounts[hash] > 1 {
			nodeKind = NodeStyleFork
		}

		// C. Prepare incoming lines from top
		var lines []VisualLine

		// Through-lines for all other active lanes not terminating or converging at this node
		for col, l := range topLanes {
			if l.hash != "" && col != nodeCol {
				// Check if this lane is converging into nodeCol
				converging := false
				for i := 1; i < len(matchingCols); i++ {
					if matchingCols[i] == col {
						converging = true
						break
					}
				}
				if converging {
					// Merge In: curve from col top to nodeCol center
					lines = append(lines, VisualLine{
						Kind:    LineIn,
						FromCol: col,
						ToCol:   nodeCol,
						Color:   laneHex(l.color),
					})
					// Close this lane
					activeLanes[col] = laneInfo{}
				} else {
					// Straight through line
					lines = append(lines, VisualLine{
						Kind:    LineThrough,
						FromCol: col,
						ToCol:   col,
						Color:   laneHex(l.color),
					})
				}
			}
		}

		// Direct top-down connection to node
		if len(matchingCols) > 0 {
			lines = append(lines, VisualLine{
				Kind:    LineIn,
				FromCol: nodeCol,
				ToCol:   nodeCol,
				Color:   laneHex(nodeColorIdx),
			})
		}

		// D. Process parents and establish outgoing lines to bottom
		parents := c.Parents
		if len(parents) == 0 {
			// Root commit: terminate lane
			activeLanes[nodeCol] = laneInfo{}
		} else {
			// First parent continues on nodeCol
			p0 := parents[0]

			// Check if p0 is already tracked in an existing active lane
			existingP0Col := -1
			for i, l := range activeLanes {
				if i != nodeCol && l.hash == p0 {
					existingP0Col = i
					break
				}
			}

			if existingP0Col != -1 {
				// Connect directly from nodeCol to existing lane
				lines = append(lines, VisualLine{
					Kind:    LineOut,
					FromCol: nodeCol,
					ToCol:   existingP0Col,
					Color:   laneHex(activeLanes[existingP0Col].color),
				})
				activeLanes[nodeCol] = laneInfo{}
			} else {
				activeLanes[nodeCol] = laneInfo{hash: p0, color: nodeColorIdx}
				lines = append(lines, VisualLine{
					Kind:    LineOut,
					FromCol: nodeCol,
					ToCol:   nodeCol,
					Color:   laneHex(nodeColorIdx),
				})
			}

			// Additional parents (merges)
			for i := 1; i < len(parents); i++ {
				p := parents[i]
				existingCol := -1
				for i, l := range activeLanes {
					if l.hash == p {
						existingCol = i
						break
					}
				}
				if existingCol != -1 {
					lines = append(lines, VisualLine{
						Kind:    LineOut,
						FromCol: nodeCol,
						ToCol:   existingCol,
						Color:   laneHex(activeLanes[existingCol].color),
					})
				} else {
					// Claim leftmost free column
					newCol := -1
					for i, l := range activeLanes {
						if l.hash == "" {
							newCol = i
							break
						}
					}
					if newCol == -1 {
						newCol = len(activeLanes)
						activeLanes = append(activeLanes, laneInfo{})
					}
					newColor := newCol
					activeLanes[newCol] = laneInfo{hash: p, color: newColor}
					lines = append(lines, VisualLine{
						Kind:    LineOut,
						FromCol: nodeCol,
						ToCol:   newCol,
						Color:   laneHex(newColor),
					})
				}
			}
		}

		// Trim trailing empty lanes
		for len(activeLanes) > 0 && activeLanes[len(activeLanes)-1].hash == "" {
			activeLanes = activeLanes[:len(activeLanes)-1]
		}

		// Track maximum columns used across all lines in this row
		rowMaxCol := nodeCol
		for _, ln := range lines {
			if ln.FromCol > rowMaxCol {
				rowMaxCol = ln.FromCol
			}
			if ln.ToCol > rowMaxCol {
				rowMaxCol = ln.ToCol
			}
		}
		if rowMaxCol+1 > maxObservedCols {
			maxObservedCols = rowMaxCol + 1
		}

		row := GraphRow{
			Commit:   c,
			IsHead:   isHead,
			NodeKind: nodeKind,
			Col:      nodeCol,
			Color:    laneColor(nodeColorIdx),
			HexColor: laneHex(nodeColorIdx),
			Lines:    lines,
			Badges:   badges,
		}
		layout.Rows = append(layout.Rows, row)
	}

	// Compact global width: at least 1 column, max reasonable 8 columns
	if maxObservedCols < 1 {
		maxObservedCols = 1
	}
	layout.MaxCols = min(maxObservedCols, 10)
	layout.Width = float32(layout.MaxCols)*graphColStep + 10

	// 3. Render precomputed SVG for every row with exact width and alignment
	for i := range layout.Rows {
		renderRowSmoothSVG(&layout.Rows[i], layout.Width)
	}

	return layout
}

// renderRowSmoothSVG builds a pixel-perfect, smooth SVG curve graphic for a row.
func renderRowSmoothSVG(row *GraphRow, totalWidth float32) {
	var sb strings.Builder
	wInt := int(totalWidth)
	if wInt < 24 {
		wInt = 24
	}
	hInt := int(graphRowHeight)

	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, wInt, hInt, wInt, hInt)

	nodeX := float32(row.Col)*graphColStep + graphColStep/2
	nodeY := graphRowHeight / 2

	// 1. Draw all lines
	for _, ln := range row.Lines {
		x1 := float32(ln.FromCol)*graphColStep + graphColStep/2
		x2 := float32(ln.ToCol)*graphColStep + graphColStep/2

		switch ln.Kind {
		case LineThrough:
			// Vertical pass-through
			fmt.Fprintf(&sb, `<line x1="%.1f" y1="0" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1.8" stroke-linecap="round"/>`, x1, x1, graphRowHeight, ln.Color)

		case LineIn:
			if x1 == x2 {
				// Vertical straight into node center
				fmt.Fprintf(&sb, `<line x1="%.1f" y1="0" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1.8" stroke-linecap="round"/>`, x1, x1, nodeY, ln.Color)
			} else {
				// Smooth S-curve converging into node center: (x1, 0) -> (x2, nodeY)
				fmt.Fprintf(&sb, `<path d="M %.1f 0 C %.1f %.1f, %.1f %.1f, %.1f %.1f" stroke="%s" stroke-width="1.8" fill="none" stroke-linecap="round"/>`,
					x1, x1, nodeY*0.5, x2, nodeY*0.7, x2, nodeY, ln.Color)
			}

		case LineOut:
			if x1 == x2 {
				// Vertical straight down from node center
				fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1.8" stroke-linecap="round"/>`, x1, nodeY, x1, graphRowHeight, ln.Color)
			} else {
				// Smooth S-curve diverging from node center to bottom: (x1, nodeY) -> (x2, H)
				fmt.Fprintf(&sb, `<path d="M %.1f %.1f C %.1f %.1f, %.1f %.1f, %.1f %.1f" stroke="%s" stroke-width="1.8" fill="none" stroke-linecap="round"/>`,
					x1, nodeY, x1, nodeY+7, x2, nodeY+7, x2, graphRowHeight, ln.Color)
			}
		}
	}

	// 2. Draw differentiated node styles (Hollow, Merge Solid, Fork Halo, Head White-Center)
	switch row.NodeKind {
	case NodeStyleUncommitted:
		// Workspace uncommitted changes: clean hollow neutral ring
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="4.2" fill="#ffffff" stroke="%s" stroke-width="1.8"/>`, nodeX, nodeY, row.HexColor)

	case NodeStyleHead:
		// HEAD / Current checked-out commit: bold concentric white-core halo
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="5" fill="%s"/>`, nodeX, nodeY, row.HexColor)
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="2.5" fill="#ffffff"/>`, nodeX, nodeY)

	case NodeStyleMerge:
		// Merge commit: solid prominent larger node
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="4.5" fill="%s"/>`, nodeX, nodeY, row.HexColor)

	case NodeStyleFork:
		// Fork commit: double ring / halo node
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="5" fill="none" stroke="%s" stroke-width="1.4"/>`, nodeX, nodeY, row.HexColor)
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s"/>`, nodeX, nodeY, row.HexColor)

	default: // NodeStyleRegular
		// Standard single-parent commit: clean colored hollow ring
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3.6" fill="#ffffff" stroke="%s" stroke-width="1.8"/>`, nodeX, nodeY, row.HexColor)
	}

	sb.WriteString(`</svg>`)

	parsed := ui.MustParseSVG([]byte(sb.String()))
	row.SVG = parsed
}

// parseRefBadges converts git log %D string into structured Badges and detects if HEAD.
func parseRefBadges(refs string, currentBranch string) ([]RefBadge, bool) {
	if strings.TrimSpace(refs) == "" {
		return nil, false
	}
	parts := strings.Split(refs, ",")
	var badges []RefBadge
	isHeadCommit := false

	for _, raw := range parts {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		isHead := false
		if strings.HasPrefix(s, "HEAD -> ") {
			isHead = true
			isHeadCommit = true
			s = strings.TrimPrefix(s, "HEAD -> ")
		} else if s == "HEAD" {
			isHeadCommit = true
			continue
		}

		var kind BadgeKind
		var col ui.Color
		switch {
		case strings.HasPrefix(s, "tag: "):
			kind = BadgeTag
			s = strings.TrimPrefix(s, "tag: ")
			col = ui.Hex("#e5c07b") // Gold
		case strings.HasPrefix(s, "origin/") || strings.HasPrefix(s, "upstream/"):
			kind = BadgeRemoteBranch
			col = ui.Hex("#56b6c2") // Cyan
		default:
			kind = BadgeLocalBranch
			if isHead || s == currentBranch {
				col = ui.Hex("#d19a66") // Primary Accent
			} else {
				col = ui.Hex("#61afef") // Blue
			}
		}

		badges = append(badges, RefBadge{
			Name:    s,
			Kind:    kind,
			Color:   col,
			BgColor: col.Alpha(0.14),
		})
	}
	return badges, isHeadCommit
}
