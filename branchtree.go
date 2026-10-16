package main

import (
	"slices"
	"strings"

	"github.com/egoist/godiff/internal/git"
)

// BranchTreeNode represents a node in the branch/tag tree hierarchy.
type BranchTreeNode struct {
	Name      string
	FullPath  string
	Ref       *git.Ref
	IsLeaf    bool
	IsCurrent bool
	Children  []*BranchTreeNode
}

// BuildBranchTrees builds three root trees: Local Branches, Remote Branches, and Tags.
func BuildBranchTrees(refs []git.Ref, currentBranch string, filter string) (branches, remotes, tags []*BranchTreeNode) {
	q := strings.ToLower(strings.TrimSpace(filter))

	// Helper to insert a path like "feature/foo/bar" into a tree
	var insertNode func(roots []*BranchTreeNode, pathParts []string, prefix string, ref git.Ref, isCurrent bool) []*BranchTreeNode
	insertNode = func(roots []*BranchTreeNode, pathParts []string, prefix string, ref git.Ref, isCurrent bool) []*BranchTreeNode {
		if len(pathParts) == 0 {
			return roots
		}
		curName := pathParts[0]
		isLeaf := len(pathParts) == 1
		full := curName
		if prefix != "" {
			full = prefix + "/" + curName
		}

		var node *BranchTreeNode
		for _, r := range roots {
			if r.Name == curName && r.IsLeaf == isLeaf {
				node = r
				break
			}
		}

		if node == nil {
			node = &BranchTreeNode{
				Name:      curName,
				FullPath:  full,
				IsLeaf:    isLeaf,
				IsCurrent: isLeaf && isCurrent,
			}
			if isLeaf {
				refCopy := ref
				node.Ref = &refCopy
			}
			roots = append(roots, node)
		}

		if !isLeaf {
			node.Children = insertNode(node.Children, pathParts[1:], full, ref, isCurrent)
		}
		return roots
	}

	for _, r := range refs {
		short := r.Short
		if q != "" && !strings.Contains(strings.ToLower(short), q) {
			continue
		}

		isCur := (r.Short == currentBranch || r.FullName == "refs/heads/"+currentBranch)

		switch r.Kind {
		case git.RefLocalBranch:
			parts := strings.Split(short, "/")
			branches = insertNode(branches, parts, "", r, isCur)
		case git.RefRemoteBranch:
			parts := strings.Split(short, "/")
			remotes = insertNode(remotes, parts, "", r, false)
		case git.RefTag:
			parts := strings.Split(short, "/")
			tags = insertNode(tags, parts, "", r, false)
		}
	}

	sortNodes(branches)
	sortNodes(remotes)
	sortNodes(tags)
	return branches, remotes, tags
}

func sortNodes(nodes []*BranchTreeNode) {
	slices.SortFunc(nodes, func(a, b *BranchTreeNode) int {
		// Folders first, then leaves
		if a.IsLeaf != b.IsLeaf {
			if !a.IsLeaf {
				return -1
			}
			return 1
		}
		// "main" or "master" top within leaves
		if a.IsLeaf && b.IsLeaf {
			if a.Name == "main" || a.Name == "master" {
				return -1
			}
			if b.Name == "main" || b.Name == "master" {
				return 1
			}
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, n := range nodes {
		if len(n.Children) > 0 {
			sortNodes(n.Children)
		}
	}
}
