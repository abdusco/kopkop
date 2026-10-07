// Package gitdates finds when files were last changed according to git history.
package gitdates

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	commitMarker   = "\x01"
	ignoreRevsFile = ".git-blame-ignore-revs"
)

// LastCommits maps slash-separated paths below dir to the committer time of
// the newest commit that touched them. Commits listed in dir's
// .git-blame-ignore-revs file are skipped, so mass reformats and moves don't
// count as edits. A missing git binary or repository yields an empty map.
func LastCommits(root, subdir string) (map[string]time.Time, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return map[string]time.Time{}, nil
	}
	if err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run(); err != nil {
		return map[string]time.Time{}, nil
	}

	cmd := exec.Command("git", "-C", root,
		"-c", "core.quotepath=false",
		"log", "--name-only", "--no-renames", "--relative",
		"--format="+commitMarker+"%H %cI",
		"--", subdir,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// An empty repository has no commits yet.
		if strings.Contains(stderr.String(), "does not have any commits") {
			return map[string]time.Time{}, nil
		}
		return nil, fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseLog(out, readIgnoredRevs(root))
}

// parseLog expects newest-first output, so the first sighting of a path wins.
func parseLog(out []byte, ignored map[string]bool) (map[string]time.Time, error) {
	dates := map[string]time.Time{}
	var current time.Time
	skip := false

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if header, ok := strings.CutPrefix(line, commitMarker); ok {
			sha, stamp, _ := strings.Cut(header, " ")
			t, err := time.Parse(time.RFC3339, stamp)
			if err != nil {
				return nil, fmt.Errorf("git log: bad commit time %q: %w", stamp, err)
			}
			current, skip = t, ignored[sha]
			continue
		}
		if skip {
			continue
		}
		if _, seen := dates[line]; !seen {
			dates[line] = current
		}
	}
	return dates, scanner.Err()
}

func readIgnoredRevs(root string) map[string]bool {
	raw, err := os.ReadFile(filepath.Join(root, ignoreRevsFile))
	if err != nil {
		return nil
	}
	revs := map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if line = strings.TrimSpace(line); line != "" {
			revs[line] = true
		}
	}
	return revs
}
