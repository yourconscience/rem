package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// wikiLinkRe captures the target of a [[target]] link and any |alias or #heading after it.
var wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]|#]+)([^\[\]]*)\]\]`)

var indexHeadingRe = regexp.MustCompile(`^#{2,5} `)

const migrateMessage = "Migrate memory to the Agent Memory Repo format"

type migrateStats struct {
	Moved bool // memory/_index.md became MEMORY.md
	Pages int  // pages whose links changed
	Links int  // links rewritten, in the index and in pages
}

// cmdMigrate moves an older vault to the Agent Memory Repo format on a branch.
// It makes no model call, never writes to main and never pushes.
func cmdMigrate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("rem migrate takes no arguments")
	}
	root := knowledgeDir()
	if prefix, err := gitIn(root, "rev-parse", "--show-prefix"); err != nil || strings.TrimSpace(prefix) != "" {
		return fmt.Errorf("%s is not the root of a git repository; migrate needs a git vault with a main branch", root)
	}
	if out, err := gitIn(root, "rev-parse", "--verify", "-q", "refs/heads/main"); err != nil {
		return fmt.Errorf("the vault has no main branch (%s)", strings.TrimSpace(out))
	}
	if _, err := gitIn(root, "cat-file", "-e", "main:MEMORY.md"); err == nil {
		fmt.Println("rem migrate: already migrated, MEMORY.md is on main; nothing changed")
		return nil
	}
	if _, err := gitIn(root, "cat-file", "-e", "main:memory/_index.md"); err != nil {
		fmt.Println("rem migrate: nothing to migrate, main has no memory/_index.md; nothing changed")
		return nil
	}
	status, err := gitIn(root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w (%s)", err, status)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("refusing: the vault has uncommitted changes; commit them (rem sync) and run rem migrate again\n%s", truncateLines(status, 5))
	}

	date := today()
	branch := "rem-migrate/" + date
	worktree := filepath.Join(root, ".worktrees", "rem-migrate-"+date)
	if err := ensureExcluded(root, ".worktrees/"); err != nil {
		return err
	}
	if out, err := gitIn(root, "worktree", "add", "-q", "-b", branch, worktree, "main"); err != nil {
		return fmt.Errorf("create %s: %w (%s); merge or delete that branch first", branch, err, out)
	}
	stats, err := migrateTree(worktree)
	if err == nil {
		err = commitAll(worktree, migrateMessage)
	}
	if err == nil {
		if out, rmErr := gitIn(root, "worktree", "remove", worktree); rmErr != nil {
			err = fmt.Errorf("remove worktree %s: %w (%s)", worktree, rmErr, out)
		}
	}
	if err != nil {
		gitIn(root, "worktree", "remove", "--force", worktree)
		gitIn(root, "branch", "-D", branch)
		return fmt.Errorf("migrate: %w; nothing kept", err)
	}

	stat, _ := gitIn(root, "diff", "--stat", "main..."+branch)
	fmt.Printf("%s\nrem migrate: %d links rewritten in %d pages; memory/_index.md moved to MEMORY.md\n", strings.TrimSpace(stat), stats.Links, stats.Pages)
	fmt.Printf("branch: %s (nothing merged, nothing pushed)\n", branch)
	fmt.Printf("review: git -C \"%s\" diff main...%s\n", root, branch)
	fmt.Printf("merge, with main checked out: git -C \"%s\" merge %s\n", root, branch)
	return nil
}

// migrateTree rewrites the vault checkout in dir: memory/_index.md becomes MEMORY.md in
// the new shape, and [[slug]] links become [[memory/slug]] in the index and in pages.
// It does no git work. A checkout with MEMORY.md, or without memory/_index.md, is left alone.
func migrateTree(dir string) (migrateStats, error) {
	var stats migrateStats
	memory := filepath.Join(dir, "memory")
	indexPath := filepath.Join(memory, "_index.md")
	if _, err := os.Stat(filepath.Join(dir, "MEMORY.md")); err == nil {
		return stats, nil
	}
	old, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}

	for _, path := range memoryPages(memory) {
		data, err := os.ReadFile(path)
		if err != nil {
			return stats, err
		}
		text, n := rewriteLinks(string(data), memory)
		if n == 0 {
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return stats, err
		}
		stats.Pages++
		stats.Links += n
	}

	index, n := rewriteLinks(string(old), memory)
	stats.Links += n
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(indexToMemory(index)), 0o644); err != nil {
		return stats, err
	}
	if err := os.Remove(indexPath); err != nil {
		return stats, err
	}
	stats.Moved = true
	return stats, nil
}

// rewriteLinks turns [[slug]] into [[memory/slug]] when memory/slug.md exists. Links
// already in the new form, and links to nothing in memory/, stay as they are.
func rewriteLinks(text, memory string) (string, int) {
	n := 0
	out := wikiLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		parts := wikiLinkRe.FindStringSubmatch(link)
		target := strings.TrimSpace(parts[1])
		switch {
		case target == "_index":
			target = "MEMORY"
		case strings.HasPrefix(target, "memory/"):
			return link
		default:
			if _, err := os.Stat(filepath.Join(memory, filepath.FromSlash(target)+".md")); err != nil {
				return link
			}
			target = "memory/" + target
		}
		n++
		return "[[" + target + parts[2] + "]]"
	})
	return out, n
}

// indexToMemory reshapes an older index: a `# Memory` title, any text that sat above the
// first `## ` heading, then `## Index` with each `## <group>` heading one level deeper.
func indexToMemory(old string) string {
	lines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		lines = lines[1:]
	}
	split := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			split = i
			break
		}
	}
	intro, groups := lines[:split], lines[split:]
	if split == len(lines) {
		intro, groups = nil, lines
	}
	for i, line := range groups {
		if indexHeadingRe.MatchString(line) {
			groups[i] = "#" + line
		}
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	if intro := strings.Trim(strings.Join(intro, "\n"), "\n"); intro != "" {
		b.WriteString(intro + "\n\n")
	}
	b.WriteString("## Index\n\n")
	b.WriteString(strings.Trim(strings.Join(groups, "\n"), "\n") + "\n")
	return b.String()
}

// commitAll stages and commits everything in a worktree that holds only a migration.
func commitAll(dir, message string) error {
	if out, err := gitIn(dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w (%s)", err, out)
	}
	if out, err := gitIn(dir, "commit", "-q", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w (%s)", err, out)
	}
	return nil
}

func truncateLines(text string, max int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > max {
		lines = append(lines[:max], fmt.Sprintf("... and %d more", len(lines)-max))
	}
	return strings.Join(lines, "\n")
}
