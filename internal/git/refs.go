package git

import (
	"bytes"
	"strconv"
	"strings"
)

// RefKind defines the type of a git reference.
type RefKind int

const (
	RefLocalBranch RefKind = iota
	RefRemoteBranch
	RefTag
)

// Ref represents a git branch, remote branch, or tag.
type Ref struct {
	FullName string  // e.g. "refs/heads/main", "refs/remotes/origin/main", "refs/tags/v1.0"
	Short    string  // e.g. "main", "origin/main", "v1.0"
	Hash     string  // full commit hash
	Kind     RefKind // Local, Remote, or Tag
	IsHead   bool    // true if current checked out HEAD branch
}

// Refs returns all local branches, remote branches, and tags in the repository.
func (r *Repo) Refs() ([]Ref, error) {
	out, err := r.Git("for-each-ref", "--format=%(refname)\x1f%(refname:short)\x1f%(objectname)\x1f%(HEAD)", "refs/heads/", "refs/remotes/", "refs/tags/")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		parts := strings.Split(string(line), "\x1f")
		if len(parts) < 3 {
			continue
		}
		fullName := parts[0]
		shortName := parts[1]
		hash := parts[2]
		isHead := len(parts) >= 4 && strings.TrimSpace(parts[3]) == "*"

		var kind RefKind
		switch {
		case strings.HasPrefix(fullName, "refs/heads/"):
			kind = RefLocalBranch
		case strings.HasPrefix(fullName, "refs/remotes/"):
			// Ignore remote HEAD pointers like origin/HEAD
			if strings.HasSuffix(fullName, "/HEAD") {
				continue
			}
			kind = RefRemoteBranch
		case strings.HasPrefix(fullName, "refs/tags/"):
			kind = RefTag
		default:
			continue
		}

		refs = append(refs, Ref{
			FullName: fullName,
			Short:    shortName,
			Hash:     hash,
			Kind:     kind,
			IsHead:   isHead,
		})
	}
	return refs, nil
}

// LogAll returns commits of all branches and tags in topological / date order.
func (r *Repo) LogAll(skip, n int) ([]Commit, error) {
	if !r.HasHead() {
		return nil, nil
	}
	args := []string{"log", "--no-color", "--format=" + logFormat, "--all", "--date-order"}
	if skip > 0 {
		args = append(args, "--skip="+strconv.Itoa(skip))
	}
	if n > 0 {
		args = append(args, "-n", strconv.Itoa(n))
	}
	args = append(args, "--")
	out, err := r.Git(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}
