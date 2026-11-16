package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// candidateLine is a parsed capture line from ai/*.md.
type candidateLine struct {
	Text string // fact text, provenance suffix stripped
	Src  string // provenance, e.g. "claude", "codex"; empty if none
	File string // path of the ai/ file it came from
	Day  string // YYYY-MM-DD derived from filename
}

var candidateRe = regexp.MustCompile(`^- candidate: (.*?)(?: \(via ([^)]+)\))?$`)

var (
	nonWordRe = regexp.MustCompile(`[^\p{L}\p{N}+]+`)
	nonSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)
)

// normalize lowercases, strips punctuation, and collapses whitespace.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonWordRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// knowledgeDir is the vault: KNOWLEDGE_DIR, else rem/vault under the XDG data
// directory. Only a command that writes creates it, through ensureVault.
func knowledgeDir() string {
	if d := os.Getenv("KNOWLEDGE_DIR"); d != "" {
		return d
	}
	return filepath.Join(getenvDefault("XDG_DATA_HOME", filepath.Join(homeDir(), ".local", "share")), "rem", "vault")
}

// ensureVault creates a missing vault before the first write and prints where it is.
func ensureVault(root string) error {
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	fmt.Printf("rem: created vault at %s\n", root)
	return nil
}

func today() string { return time.Now().Format("2006-01-02") }

// cmdAdd writes one note to memory/_inbox, skipping facts already captured there or
// in the legacy ai/ queue. Inbox notes are searchable at once and never injected.
func cmdAdd(args []string) error {
	src := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)
		case a == "-src" || a == "--src":
			if i+1 == len(args) {
				return fmt.Errorf("%s needs a harness name", a)
			}
			src = args[i+1]
			i++
		case len(a) > 1 && strings.HasPrefix(a, "-"):
			// A typo'd flag must not become a note; -- lets a fact start with "-".
			return fmt.Errorf("unknown flag %q (put -- before a fact that starts with -)", a)
		default:
			rest = append(rest, a)
		}
	}
	fact := redactText(strings.TrimSpace(strings.Join(rest, " ")))
	if fact == "" {
		return fmt.Errorf("usage: rem add [-src harness] \"fact\"")
	}

	root := knowledgeDir()
	inbox := filepath.Join(root, "memory", "_inbox")
	notes, err := loadInbox(inbox)
	if err != nil {
		return err
	}
	fp := normalize(fact)
	if fp != "" {
		for _, n := range notes {
			if normalize(n.Body) == fp {
				fmt.Printf("rem add: already captured in %s\n", n.Path)
				return nil
			}
		}
		legacy, err := loadCandidates(filepath.Join(root, "ai"))
		if err != nil {
			return err
		}
		for _, c := range legacy {
			if normalize(c.Text) == fp {
				fmt.Printf("rem add: already captured on %s: %s\n", c.Day, c.Text)
				return nil
			}
		}
	}

	if err := ensureVault(root); err != nil {
		return err
	}
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	now := time.Now().UTC()
	path := filepath.Join(inbox, noteFileName(now, fact))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(renderNote(fact, src, cwd, now)); err != nil {
		return err
	}
	fmt.Printf("rem add: captured to %s (unreviewed until a dream merges it)\n", path)
	return nil
}

// inboxNote is one parsed memory/_inbox file.
type inboxNote struct {
	Path, Body, Harness, Cwd, Created string
}

func noteFileName(now time.Time, fact string) string {
	words := strings.Fields(normalize(fact))
	if len(words) > 6 {
		words = words[:6]
	}
	slug := strings.Trim(nonSlugRe.ReplaceAllString(strings.Join(words, "-"), ""), "-")
	if slug == "" {
		slug = "note"
	}
	sum := sha256.Sum256([]byte(fact))
	return fmt.Sprintf("%s-%s-%x.md", now.Format("20060102T150405Z"), slug, sum[:2])
}

func renderNote(fact, src, cwd string, now time.Time) string {
	if src == "" {
		src = "manual"
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", strings.TrimSuffix(noteFileName(now, fact), ".md"))
	fmt.Fprintf(&b, "description: %s\n", strconv.Quote(truncate(fact, 120)))
	b.WriteString("metadata:\n  type: note\n  status: unreviewed\n")
	fmt.Fprintf(&b, "  harness: %s\n  cwd: %s\n  created: %s\n", strconv.Quote(src), strconv.Quote(cwd), now.Format(time.RFC3339))
	b.WriteString("---\n\n")
	b.WriteString(fact)
	b.WriteString("\n")
	return b.String()
}

func frontmatterValue(frontmatter, key string) string {
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		return value
	}
	return ""
}

func loadInbox(dir string) ([]inboxNote, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	notes := make([]inboxNote, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		note := inboxNote{Path: path, Body: strings.TrimSpace(string(data))}
		if rest, ok := strings.CutPrefix(string(data), "---\n"); ok {
			if frontmatter, body, ok := strings.Cut(rest, "\n---\n"); ok {
				note.Body = strings.TrimSpace(body)
				note.Harness = frontmatterValue(frontmatter, "harness")
				note.Cwd = frontmatterValue(frontmatter, "cwd")
				note.Created = frontmatterValue(frontmatter, "created")
			}
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// loadCandidates parses every `- candidate:` line across ai/*.md.
func loadCandidates(dir string) ([]candidateLine, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []candidateLine
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, " \t\r")
			m := candidateRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			out = append(out, candidateLine{
				Text: strings.TrimSpace(m[1]),
				Src:  m[2],
				File: filepath.Join(dir, name),
				Day:  strings.TrimSuffix(name, ".md"),
			})
		}
	}
	return out, nil
}

// cmdSearch prints unreviewed inbox matches, then memsearch results with their
// paths. Flags pass straight through to memsearch.
func cmdSearch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: rem search \"query\"")
	}
	collection := os.Getenv("REM_COLLECTION")
	if collection == "" {
		collection = "ai"
	}
	bin, lookErr := exec.LookPath("memsearch")
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if lookErr != nil {
				return fmt.Errorf("memsearch not found: %w", lookErr)
			}
			cmd := exec.Command(bin, searchArgs(args, collection)...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		}
	}

	query := strings.Join(args, " ")
	shown := map[string]bool{}
	notes, err := loadInbox(filepath.Join(knowledgeDir(), "memory", "_inbox"))
	if err != nil {
		return err
	}
	for _, n := range matchInbox(notes, query, 5) {
		fmt.Printf("[unreviewed] %s\n%s\n\n", n.Path, truncate(n.Body, 500))
		shown[n.Path] = true
	}
	if lookErr != nil {
		if len(shown) > 0 {
			fmt.Fprintln(os.Stderr, "rem search: memsearch not found, showing inbox matches only")
			return nil
		}
		return fmt.Errorf("memsearch not found: %w", lookErr)
	}
	out, err := exec.Command(bin, append(searchArgs(nil, collection), "--json-output", query)...).Output()
	if err != nil {
		if len(shown) > 0 {
			fmt.Fprintf(os.Stderr, "rem search: memsearch failed (%v), showing inbox matches only\n", err)
			return nil
		}
		return fmt.Errorf("memsearch: %w", err)
	}
	var hits []struct {
		Content string  `json:"content"`
		Source  string  `json:"source"`
		Line    int     `json:"start_line"`
		Score   float64 `json:"score"`
	}
	if err := json.Unmarshal(out, &hits); err != nil {
		return fmt.Errorf("memsearch output: %w", err)
	}
	for _, h := range hits {
		if shown[h.Source] {
			continue
		}
		label := ""
		if strings.Contains(h.Source, "/memory/_inbox/") {
			label = "[unreviewed] "
		}
		fmt.Printf("%s%s:%d (score %.2f)\n%s\n\n", label, h.Source, h.Line, h.Score, strings.TrimSpace(h.Content))
	}
	return nil
}

// matchInbox ranks notes by shared query words; a note needs at least half of them.
// It covers notes written since the last reindex, so a capture is findable at once.
func matchInbox(notes []inboxNote, query string, limit int) []inboxNote {
	words := tokens(query)
	need := (len(words) + 1) / 2
	type scored struct {
		note  inboxNote
		score int
	}
	var matched []scored
	for _, n := range notes {
		body := tokens(n.Body)
		score := 0
		for w := range words {
			if body[w] {
				score++
			}
		}
		if score > 0 && score >= need {
			matched = append(matched, scored{n, score})
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].score > matched[j].score })
	if len(matched) > limit {
		matched = matched[:limit]
	}
	out := make([]inboxNote, len(matched))
	for i, m := range matched {
		out[i] = m.note
	}
	return out
}

// searchArgs builds the memsearch argv, injecting `--collection <collection>`
// unless the caller already passed a collection flag.
func searchArgs(args []string, collection string) []string {
	for _, a := range args {
		if a == "-c" || a == "--collection" {
			return append([]string{"search"}, args...)
		}
	}
	out := []string{"search"}
	if collection != "" {
		out = append(out, "--collection", collection)
	}
	return append(out, args...)
}

// cmdSync wraps the guarded knowledge-sync binary.
func cmdSync() error {
	bin := os.Getenv("REM_SYNC_BIN")
	if bin == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		bin = filepath.Join(home, ".local", "bin", "knowledge-sync")
		// go install and release archives put it elsewhere on PATH.
		if _, err := os.Stat(bin); err != nil {
			if found, err := exec.LookPath("knowledge-sync"); err == nil {
				bin = found
			}
		}
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("knowledge-sync not found at %s: %w", bin, err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
