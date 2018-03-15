// Copyright 2017 Atelier Disko. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fatih/color"
)

var (
	// Directory basenames matching the pattern are not descending into
	// and interpreted as a node.
	IgnoreNodesRegexp = regexp.MustCompile(`^(x[-_].*|\..*|node_modules)$`)
)

type NodeTree struct {
	// The absolute root path of the tree.
	path string

	// Maps node URL paths to nodes, for quick lookup.
	lookup map[string]*Node

	// The root node and entry point to the acutal tree.
	Root *Node `json:"root"`

	// Authors database, if AUTHORS.txt file exists.
	authors *Authors
}

// Returns an unsynced tree from path; you must initialize the Tree
// using Sync() before using it.
func NewNodeTreeFromPath(path string) *NodeTree {
	return &NodeTree{path: path}
}

// One-way sync: updates tree from file system. Recursively crawls
// the given root directory, constructing a tree of nodes. Does not
// support symlinks inside the tree.
func (t *NodeTree) Sync() error {
	yellow := color.New(color.FgYellow).SprintFunc()

	var nodes []*Node

	err := filepath.Walk(t.path, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if f.IsDir() {
			if IgnoreNodesRegexp.MatchString(f.Name()) {
				log.Printf("Ignoring node: %s", yellow(prettyPath(path)))
				return filepath.SkipDir
			}
			n, nErr := NewNodeFromPath(path, t.path)
			if nErr != nil {
				return nErr
			}
			if n.IsGhost {
				log.Printf("Ghosted node: %s", yellow(nErr))
			}
			nodes = append(nodes, n)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to walk directory tree %s: %s", t.path, err)
	}

	// In the second pass we're doing two thing: add the children
	// to the nodes and build up a lookup table, as we're already
	// iterating the nodes.
	lookup := make(map[string]*Node)

	for _, n := range nodes {
		lookup[strings.ToLower(n.URL())] = n

		for _, sn := range nodes {
			if filepath.Dir(sn.path) == n.path {
				n.AddChild(sn)
			}
		}
	}

	// Swap late, in event of error we keep the previous state.
	t.lookup = lookup
	t.Root = lookup[""]
	log.Printf("Established tree lookup table with %d entries", len(lookup))

	// Refresh the authors database; file may appear or disappear between
	// syncs.
	authorsFile := filepath.Join(t.path, AuthorsConfigBasename)
	var as *Authors

	if _, err := os.Stat(authorsFile); err == nil {
		as, err = NewAuthorsFromFile(authorsFile)
		if err != nil {
			return err
		}
	} else {
		as = &Authors{}
	}
	t.authors = as

	return nil
}

// Determines the node following the given current node. This may
// either be the first child of the given node, if there are none
// the sibling node and - walking up the tree - if there is none the
// parents sibling node.
func (t NodeTree) NextNode(current *Node) (*Node, error) {
	urls := make([]string, 0, len(t.lookup))
	for url, _ := range t.lookup {
		urls = append(urls, url)
	}
	sort.Strings(urls)

	url := strings.ToLower(normalizeNodeURL(current.URL()))
	key := sort.SearchStrings(urls, url)

	// SearchString returns the next unused key, if the given string
	// isn't found.
	if key == len(urls) {
		return nil, fmt.Errorf("No node with URL path '%s' in tree", url)
	}
	// We don't wrap, check if this is the last node.
	if key == len(urls)-1 {
		return nil, nil
	}
	return t.Get(urls[key+1])
}

// Returns the number of total nodes in the tree.
func (t NodeTree) TotalNodes() uint16 {
	return uint16(len(t.lookup))
}

// Retrieves a node from the tree, performs a case-insensitive match.
func (t NodeTree) Get(url string) (*Node, error) {
	if n, ok := t.lookup[strings.ToLower(normalizeNodeURL(url))]; ok {
		return n, nil
	}
	return &Node{}, fmt.Errorf("No node with URL path '%s' in tree", url)
}

// Retrieves a node from tree and ensures it's synced before.
func (t NodeTree) GetSynced(url string) (*Node, error) {
	if n, ok := t.lookup[strings.ToLower(normalizeNodeURL(url))]; ok {
		if err := n.Sync(); err != nil {
			return n, err
		}
		return n, nil
	}
	return &Node{}, fmt.Errorf("No synced node with URL path '%s' in tree", url)
}

// Performs a narrow fuzzy search on the node's visible attributes
// (the title) plus tags & keywords and returns the collected results
// as a flat node list.
func (t NodeTree) FuzzySearch(query string) []*Node {
	var results []*Node

	matches := func(source string, target string) bool {
		if source == "" {
			return false
		}
		return strings.Contains(strings.ToLower(target), strings.ToLower(source))
	}

Outer:
	for _, n := range t.lookup {
		if matches(query, n.Title()) {
			results = append(results, n)
			continue Outer
		}
		if matches(query, n.Description()) {
			results = append(results, n)
			continue Outer
		}
		for _, v := range n.Tags() {
			if matches(query, v) {
				results = append(results, n)
				continue Outer
			}
		}
		for _, v := range n.Keywords() {
			if matches(query, v) {
				results = append(results, n)
				continue Outer
			}
		}
	}

	log.Printf("Fuzzy searched tree for '%s' with %d results", query, len(results))
	return results
}