package ui

import (
	"path"
	"sort"
	"strings"

	"foldersnap/internal/model"
)

// diffTreeRow is a visible row in the comparison tree. Parent folders that
// did not themselves change are represented by nodes without an entry.
type diffTreeRow struct {
	Path, Name string
	Entry      *model.DiffEntry
	FolderSize model.FolderSize
	Depth      int
	Directory  bool
	Expanded   bool
	CanExpand  bool
}

type diffTreeNode struct {
	path, identity, name string
	entry                *model.DiffEntry
	folderSize           model.FolderSize
	directory            bool
	children             map[string]*diffTreeNode
}

type diffTreeView struct {
	Rows    []diffTreeRow
	Changes int
}

func makeDiffTreeView(entries []model.DiffEntry, folderSizes map[string]model.FolderSize, filter model.ChangeType, query string, expanded map[string]bool) diffTreeView {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := func(entry model.DiffEntry) bool {
		if entry.Uncertain || entry.ScopeDifference || entry.Change == model.ChangeUnchanged {
			return false
		}
		if filter != "" && entry.Change != filter {
			return false
		}
		return query == "" || strings.Contains(strings.ToLower(entry.DisplayPath), query)
	}
	root := &diffTreeNode{directory: true, children: map[string]*diffTreeNode{}}
	changes := 0
	for index := range entries {
		entry := &entries[index]
		if !matches(*entry) {
			continue
		}
		changes++
		parts := strings.Split(strings.Trim(entry.DisplayPath, "/"), "/")
		identityPath := strings.Trim(entry.Path, "/")
		if identityPath == "" {
			identityPath = strings.ToLower(strings.Trim(entry.DisplayPath, "/"))
		}
		identityParts := strings.Split(identityPath, "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		node := root
		for partIndex, part := range parts {
			identityPart := strings.ToLower(part)
			if partIndex < len(identityParts) {
				identityPart = strings.ToLower(identityParts[partIndex])
			}
			child := node.children[identityPart]
			if child == nil {
				child = &diffTreeNode{name: part, path: path.Join(node.path, part), identity: path.Join(node.identity, identityPart), directory: partIndex < len(parts)-1, children: map[string]*diffTreeNode{}}
				node.children[identityPart] = child
			}
			node = child
		}
		node.entry = entry
		node.directory = node.directory || diffEntryIsDirectory(*entry)
	}
	var assignFolderSizes func(*diffTreeNode)
	assignFolderSizes = func(parent *diffTreeNode) {
		for _, node := range parent.children {
			if node.directory {
				node.folderSize = folderSizes[node.identity]
			}
			assignFolderSizes(node)
		}
	}
	assignFolderSizes(root)
	view := diffTreeView{Changes: changes}
	var appendChildren func(*diffTreeNode, int)
	appendChildren = func(parent *diffTreeNode, depth int) {
		children := sortedDiffTreeChildren(parent)
		for _, node := range children {
			canExpand := len(node.children) > 0
			isExpanded := expanded[node.path]
			view.Rows = append(view.Rows, diffTreeRow{Path: node.path, Name: node.name, Entry: node.entry, FolderSize: node.folderSize, Depth: depth, Directory: node.directory, CanExpand: canExpand, Expanded: isExpanded})
			if canExpand && isExpanded {
				appendChildren(node, depth+1)
			}
		}
	}
	appendChildren(root, 0)
	return view
}

func sortedDiffTreeChildren(parent *diffTreeNode) []*diffTreeNode {
	children := make([]*diffTreeNode, 0, len(parent.children))
	for _, child := range parent.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		left, right := children[i], children[j]
		if left.directory != right.directory {
			return left.directory
		}
		if !left.directory {
			leftSize, rightSize := diffTreeFileSize(left), diffTreeFileSize(right)
			if leftSize != rightSize {
				return leftSize > rightSize
			}
		}
		leftName, rightName := strings.ToLower(left.name), strings.ToLower(right.name)
		if leftName != rightName {
			return leftName < rightName
		}
		return left.name < right.name
	})
	return children
}

func diffTreeFileSize(node *diffTreeNode) int64 {
	if node.entry == nil {
		return 0
	}
	if node.entry.After != nil && node.entry.After.Type == model.EntryFile {
		return node.entry.After.Size
	}
	if node.entry.Before != nil && node.entry.Before.Type == model.EntryFile {
		return node.entry.Before.Size
	}
	return 0
}

func diffEntryIsDirectory(entry model.DiffEntry) bool {
	return (entry.After != nil && entry.After.Type == model.EntryDirectory) || (entry.Before != nil && entry.Before.Type == model.EntryDirectory)
}
