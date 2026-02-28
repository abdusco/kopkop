package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Difference struct {
	Path   string
	Reason string
}

func CollectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func CompareDirectories(leftRoot string, rightRoot string, ignore []string) ([]Difference, error) {
	leftFiles, err := CollectFiles(leftRoot)
	if err != nil {
		return nil, fmt.Errorf("collect left files: %w", err)
	}
	rightFiles, err := CollectFiles(rightRoot)
	if err != nil {
		return nil, fmt.Errorf("collect right files: %w", err)
	}

	leftSet := toSet(filterIgnored(leftFiles, ignore))
	rightSet := toSet(filterIgnored(rightFiles, ignore))

	all := make([]string, 0, len(leftSet)+len(rightSet))
	seen := map[string]struct{}{}
	for p := range leftSet {
		all = append(all, p)
		seen[p] = struct{}{}
	}
	for p := range rightSet {
		if _, ok := seen[p]; !ok {
			all = append(all, p)
		}
	}
	sort.Strings(all)

	var diffs []Difference
	for _, rel := range all {
		_, inLeft := leftSet[rel]
		_, inRight := rightSet[rel]
		if !inLeft {
			diffs = append(diffs, Difference{Path: rel, Reason: "missing in left"})
			continue
		}
		if !inRight {
			diffs = append(diffs, Difference{Path: rel, Reason: "missing in right"})
			continue
		}

		leftPath := filepath.Join(leftRoot, filepath.FromSlash(rel))
		rightPath := filepath.Join(rightRoot, filepath.FromSlash(rel))
		leftData, err := os.ReadFile(leftPath)
		if err != nil {
			return nil, fmt.Errorf("read left file %q: %w", rel, err)
		}
		rightData, err := os.ReadFile(rightPath)
		if err != nil {
			return nil, fmt.Errorf("read right file %q: %w", rel, err)
		}

		ext := strings.ToLower(filepath.Ext(rel))
		normLeft, err := NormalizeByExt(ext, leftData)
		if err != nil {
			return nil, fmt.Errorf("normalize left %q: %w", rel, err)
		}
		normRight, err := NormalizeByExt(ext, rightData)
		if err != nil {
			return nil, fmt.Errorf("normalize right %q: %w", rel, err)
		}

		if string(normLeft) != string(normRight) {
			diffs = append(diffs, Difference{Path: rel, Reason: "content differs"})
		}
	}

	return diffs, nil
}

func toSet(paths []string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		out[p] = struct{}{}
	}
	return out
}

func filterIgnored(paths []string, ignore []string) []string {
	if len(ignore) == 0 {
		return paths
	}
	var out []string
	for _, p := range paths {
		ignored := false
		for _, i := range ignore {
			if p == i {
				ignored = true
				break
			}
			if match, _ := filepath.Match(i, p); match {
				ignored = true
				break
			}
		}
		if !ignored {
			out = append(out, p)
		}
	}
	return out
}
