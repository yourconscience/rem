package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed prompts/analyzer.md
var analyzerPrompt string

//go:embed prompts/organizer.md
var organizerPrompt string

const (
	dreamQuietPeriod   = time.Hour
	dreamDefaultWindow = "30d"
	dreamPageCap       = 100 * 1024
	dreamTranscriptCap = 400 * 1024
	dreamRunTimeout    = 40 * time.Minute
)

// Tool output is the bulkiest part of a transcript. These limits apply in turn
// until the rendered session fits dreamTranscriptCap.
var dreamToolResultCaps = []int{6000, 2000, 500}

type dreamSession struct {
	ID               string `json:"id"`
	Project          string `json:"project"`
	Machine          string `json:"machine"`
	Agent            string `json:"agent"`
	Cwd              string `json:"cwd"`
	StartedAt        string `json:"started_at"`
	EndedAt          string `json:"ended_at"`
	IsAutomated      bool   `json:"is_automated"`
	UserMessageCount int    `json:"user_message_count"`
}

type dreamToolCall struct {
	ToolName      string `json:"tool_name"`
	InputJSON     string `json:"input_json"`
	ResultContent string `json:"result_content"`
}

type dreamMessage struct {
	Ordinal   int             `json:"ordinal"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	IsSystem  bool            `json:"is_system"`
	ToolCalls []dreamToolCall `json:"tool_calls"`
}

type dreamResult struct {
	Summary    string   `json:"summary"`
	FactsAdded int      `json:"facts_added"`
	Corrected  int      `json:"corrections"`
	Conflicts  int      `json:"conflicts"`
	Merged     int      `json:"merged"`
	Entries    int      `json:"index_entries"`
	Files      []string `json:"files"`
}

type dreamProposal struct {
	Target  string   `json:"target"`
	Action  string   `json:"action"`
	Section string   `json:"section"`
	Line    string   `json:"line"`
	Why     string   `json:"why"`
	Sources []string `json:"sources"`
}

type dreamState struct {
	Watermark string `json:"watermark"`
}

type dreamInput struct {
	Key   string
	Label string
	Text  string
	Cut   bool
}

// dreamV2 reads finished sessions, runs one analyzer per session and one
// organizer on a vault branch, and stops at a commit for a person to review.
func dreamV2(args []string) error {
	fs := flag.NewFlagSet("dream --v2", flag.ContinueOnError)
	since := fs.String("since", "", "read sessions active since this date or duration instead of the watermark")
	limit := fs.Int("limit", 0, "read only the oldest N selected sessions")
	parallel := fs.Int("parallel", 4, "analyzers running at once")
	dryRun := fs.Bool("dry-run", false, "print the selection and rendered sizes, run no model")
	var only multiFlag
	fs.Var(&only, "session", "read this session id only (repeatable)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *parallel < 1 {
		return fmt.Errorf("--parallel must be at least 1")
	}
	runner := getenvDefault("REM_DREAM_RUNNER", "claude")
	if _, err := runnerArgs(runner, "probe"); err != nil {
		return err
	}

	root := knowledgeDir()
	state := readDreamState(root)
	if out, err := exec.Command("agentsview", "sync").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "rem dream: agentsview sync incomplete, continuing: %v\n%s", err, out)
	}
	window := dreamWindow(*since, state.Watermark)
	listed, err := listDreamSessions(window)
	if err != nil {
		return err
	}
	watermark := state.Watermark
	if *since != "" {
		watermark = ""
	}
	selected := selectDreamSessions(listed, watermark, only, *limit, time.Now())
	if len(selected) == 0 {
		fmt.Println("rem dream: no finished sessions to read")
		return nil
	}

	inputs := make([]dreamInput, 0, len(selected)+1)
	rendered := 0
	for _, s := range selected {
		input, err := renderDreamSession(s)
		if err != nil {
			return fmt.Errorf("render %s: %w", s.ID, err)
		}
		rendered += len(input.Text)
		inputs = append(inputs, input)
	}

	inbox, err := loadInbox(filepath.Join(root, "memory", "_inbox"))
	if err != nil {
		return err
	}
	if notes := renderNotesInput(inbox, listed, state.Watermark); notes != "" {
		rendered += len(notes)
		inputs = append(inputs, dreamInput{Key: "notes", Label: "inbox notes and harness memory", Text: notes})
	}

	if *dryRun {
		for _, in := range inputs {
			cut := ""
			if in.Cut {
				cut = " (tool output shortened)"
			}
			fmt.Printf("%8d  %s  %s%s\n", len(in.Text), in.Key, in.Label, cut)
		}
		fmt.Printf("rem dream: %d inputs, %d bytes, runner %s\n", len(inputs), rendered, runner)
		return nil
	}

	date := today()
	branch := "rem-dream/" + date
	worktree := filepath.Join(root, ".worktrees", "rem-dream-"+date)
	if err := ensureExcluded(root, ".worktrees/"); err != nil {
		return err
	}
	if out, err := gitIn(root, "worktree", "add", "-q", "-b", branch, worktree, "main"); err != nil {
		return fmt.Errorf("create %s: %w (%s); merge or delete that branch first", branch, err, out)
	}
	next := state.Watermark
	if len(only) == 0 {
		next = laterTime(next, selected[len(selected)-1].EndedAt)
	}
	return runDream(root, worktree, branch, date, runner, next, *parallel, selected, inputs, inbox, rendered)
}

func runDream(root, worktree, branch, date, runner, watermark string, parallel int, selected []dreamSession, inputs []dreamInput, inbox []inboxNote, rendered int) error {
	memory := filepath.Join(worktree, "memory")
	work := filepath.Join(memory, ".dream")
	for _, dir := range []string{"inputs", "results", "logs"} {
		if err := os.MkdirAll(filepath.Join(work, dir), 0o755); err != nil {
			return err
		}
	}
	for _, in := range inputs {
		if err := os.WriteFile(filepath.Join(work, "inputs", in.Key+".txt"), []byte(in.Text), 0o644); err != nil {
			return err
		}
	}

	results, failed := runAnalyzers(memory, runner, parallel, inputs)
	if len(failed) > 0 {
		return fmt.Errorf("coverage check failed, %d of %d inputs have no analyzer result: %s; nothing committed, see %s",
			len(failed), len(inputs), strings.Join(failed, ", "), work)
	}

	userMD := filepath.Join(worktree, "profile", "USER.md")
	agentsMD := getenvDefault("REM_AGENTS_MD", filepath.Join(homeDir(), ".agents", "AGENTS.md"))
	for src, name := range map[string]string{userMD: "USER.md", agentsMD: "AGENTS.md"} {
		if err := copyRedacted(src, filepath.Join(work, name)); err != nil {
			return err
		}
	}
	organizerTask := organizerPrompt + "\nResult file: .dream/results/organizer.json\n"
	if err := runRunner(memory, runner, "organizer", organizerTask); err != nil {
		return fmt.Errorf("organizer: %w; nothing committed, see %s", err, work)
	}
	organizer, err := readDreamResult(filepath.Join(work, "results", "organizer.json"))
	if err != nil {
		return fmt.Errorf("organizer result: %w; nothing committed, see %s", err, work)
	}
	proposals, err := readProposals(filepath.Join(work, "proposals.jsonl"))
	if err != nil {
		return fmt.Errorf("organizer proposals: %w; nothing committed, see %s", err, work)
	}

	if err := os.RemoveAll(work); err != nil {
		return err
	}
	for _, n := range inbox {
		if err := os.Remove(filepath.Join(memory, "_inbox", filepath.Base(n.Path))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if watermark != "" {
		state, err := json.Marshal(dreamState{Watermark: watermark})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(memory, ".dream-state"), append(state, '\n'), 0o644); err != nil {
			return err
		}
	}

	agentsPatch, applied, skipped, err := applyProposals(worktree, agentsMD, date, proposals)
	if err != nil {
		return err
	}
	warnings := checkMemoryPages(memory)
	report := renderDreamReport(date, branch, selected, inputs, results, organizer, proposals, skipped, warnings, memoryBytes(memory), rendered, agentsPatch)
	reportPath := filepath.Join(worktree, "reviews", "rem-dream-v2-"+date+".md")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		return err
	}

	if err := commitDream(worktree, "rem dream "+date+": read "+fmt.Sprint(len(selected))+" sessions", "memory", "reviews"); err != nil {
		return err
	}
	if applied > 0 {
		if err := commitDream(worktree, "rem dream "+date+": propose profile changes", "profile/USER.md"); err != nil {
			return err
		}
	}
	stat, _ := gitIn(root, "diff", "--stat", "main..."+branch)
	fmt.Printf("%s\nrem dream: %d sessions read, nothing merged\nbranch:   %s\nworktree: %s\nreport:   %s\n",
		strings.TrimSpace(stat), len(selected), branch, worktree, reportPath)
	return nil
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal("user home", err, "")
	}
	return home
}

func gitIn(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return string(out), err
}

func ensureExcluded(root, pattern string) error {
	path, err := gitIn(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("git path: %w", err)
	}
	exclude := strings.TrimSpace(path)
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(root, exclude)
	}
	data, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, pattern)
	return err
}

// ---- selection ------------------------------------------------------------

func readDreamState(root string) dreamState {
	var state dreamState
	if data, err := os.ReadFile(filepath.Join(root, "memory", ".dream-state")); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

// dreamWindow is the --since value for the listing. AgentsView filters on activity
// by day or duration, so a watermark widens to its whole day and selection narrows it.
func dreamWindow(since, watermark string) string {
	if since != "" {
		return since
	}
	if len(watermark) >= 10 {
		return watermark[:10]
	}
	return dreamDefaultWindow
}

// laterTime returns whichever RFC3339 value is later; an empty a loses.
func laterTime(a, b string) string {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || (errB == nil && tb.After(ta)) {
		return b
	}
	return a
}

func listDreamSessions(since string) ([]dreamSession, error) {
	var all []dreamSession
	cursor := ""
	for {
		args := []string{"session", "list", "--json", "--since", since, "--limit", "500"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		out, err := exec.Command("agentsview", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("agentsview session list: %w", err)
		}
		var page struct {
			Sessions   []dreamSession    `json:"sessions"`
			NextCursor string            `json:"next_cursor"`
			Labels     map[string]string `json:"machine_labels"`
		}
		if err := json.Unmarshal(out, &page); err != nil {
			return nil, fmt.Errorf("agentsview session list output: %w", err)
		}
		for _, s := range page.Sessions {
			if label := page.Labels[s.Machine]; label != "" {
				s.Machine = label
			}
			all = append(all, s)
		}
		if page.NextCursor == "" || len(page.Sessions) == 0 {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

func isPrivateSession(project, cwd string) bool {
	markers := getenvDefault("REM_PRIVATE_PROJECTS", "personal,telegram")
	names := append(strings.Split(cwd, string(filepath.Separator)), project)
	for _, marker := range strings.Split(markers, ",") {
		marker = strings.ToLower(strings.TrimSpace(marker))
		if marker == "" {
			continue
		}
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), marker) {
				return true
			}
		}
	}
	return false
}

// selectDreamSessions keeps finished, non-private sessions past the watermark,
// oldest first, so a limited run still moves the watermark forward in order.
func selectDreamSessions(listed []dreamSession, watermark string, only []string, limit int, now time.Time) []dreamSession {
	wanted := map[string]bool{}
	for _, id := range only {
		wanted[id] = true
	}
	var mark time.Time
	if watermark != "" {
		mark, _ = time.Parse(time.RFC3339, watermark)
	}
	var out []dreamSession
	for _, s := range listed {
		if len(wanted) > 0 && !wanted[s.ID] {
			continue
		}
		ended, err := time.Parse(time.RFC3339, s.EndedAt)
		if err != nil || now.Sub(ended) < dreamQuietPeriod {
			continue
		}
		if s.IsAutomated || s.UserMessageCount == 0 || isPrivateSession(s.Project, s.Cwd) {
			continue
		}
		if !mark.IsZero() && !ended.After(mark) && len(wanted) == 0 {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndedAt < out[j].EndedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ---- rendering ------------------------------------------------------------

var unsafeKeyRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func renderDreamSession(s dreamSession) (dreamInput, error) {
	messages, err := loadDreamMessages(s.ID)
	if err != nil {
		return dreamInput{}, err
	}
	input := dreamInput{
		Key:   unsafeKeyRe.ReplaceAllString(s.ID, "-"),
		Label: fmt.Sprintf("%s %s %s", s.Project, s.Agent, s.EndedAt),
	}
	for _, limit := range dreamToolResultCaps {
		text, cut := renderTranscript(s, messages, limit)
		input.Text, input.Cut = text, cut
		if len(text) <= dreamTranscriptCap {
			return input, nil
		}
	}
	keep := dreamTranscriptCap / 2
	input.Text = input.Text[:keep] + "\n\n[truncated: middle of this session removed to fit]\n\n" + input.Text[len(input.Text)-keep:]
	input.Text = strings.ToValidUTF8(input.Text, "")
	input.Cut = true
	return input, nil
}

func loadDreamMessages(id string) ([]dreamMessage, error) {
	var all []dreamMessage
	from := 0
	for {
		out, err := exec.Command("agentsview", "session", "messages", id, "--json", "--from", fmt.Sprint(from), "--limit", "500").Output()
		if err != nil {
			return nil, fmt.Errorf("agentsview session messages: %w", err)
		}
		var page struct {
			Messages []dreamMessage `json:"messages"`
		}
		if err := json.Unmarshal(out, &page); err != nil {
			return nil, fmt.Errorf("agentsview session messages output: %w", err)
		}
		if len(page.Messages) == 0 {
			return all, nil
		}
		all = append(all, page.Messages...)
		from = page.Messages[len(page.Messages)-1].Ordinal + 1
	}
}

func shortenMiddle(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	head := limit * 2 / 3
	tail := limit - head
	marker := fmt.Sprintf("\n[truncated %d bytes]\n", len(s)-limit)
	return strings.ToValidUTF8(s[:head]+marker+s[len(s)-tail:], ""), true
}

func renderTranscript(s dreamSession, messages []dreamMessage, resultCap int) (string, bool) {
	var b strings.Builder
	cut := false
	fmt.Fprintf(&b, "SESSION id=%s project=%s agent=%s machine=%s cwd=%s started=%s ended=%s\n\n",
		s.ID, s.Project, s.Agent, s.Machine, s.Cwd, s.StartedAt, s.EndedAt)
	for _, m := range messages {
		if m.IsSystem {
			continue
		}
		label := "AGENT"
		if m.Role == "user" {
			label = "USER"
		}
		if content := strings.TrimSpace(m.Content); content != "" {
			fmt.Fprintf(&b, "%s:\n%s\n\n", label, content)
		}
		for _, call := range m.ToolCalls {
			input, inputCut := shortenMiddle(call.InputJSON, 2000)
			result, resultCut := shortenMiddle(call.ResultContent, resultCap)
			cut = cut || inputCut || resultCut
			fmt.Fprintf(&b, "TOOL CALL %s:\n%s\n\n", call.ToolName, input)
			if result != "" {
				fmt.Fprintf(&b, "TOOL RESULT (%s):\n%s\n\n", call.ToolName, result)
			}
		}
	}
	return redactText(b.String()), cut
}

// renderNotesInput lists inbox notes, each with the session it most likely came
// from, plus harness memory files changed since the watermark.
func renderNotesInput(inbox []inboxNote, listed []dreamSession, watermark string) string {
	var b strings.Builder
	for _, n := range inbox {
		fmt.Fprintf(&b, "NOTE %s harness=%s cwd=%s created=%s likely_session=%s\n%s\n\n",
			filepath.Base(n.Path), n.Harness, n.Cwd, n.Created, resolveNoteSession(n, listed), n.Body)
	}
	var mark time.Time
	if watermark != "" {
		mark, _ = time.Parse(time.RFC3339, watermark)
	}
	home := homeDir()
	claude, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "memory", "*.md"))
	codex, _ := filepath.Glob(filepath.Join(home, ".codex", "memories", "*.md"))
	for _, path := range append(claude, codex...) {
		info, err := os.Stat(path)
		if err != nil || filepath.Base(path) == "MEMORY.md" && strings.Contains(path, ".claude") {
			continue
		}
		if !info.ModTime().After(mark) || isPrivateSession("", path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rel := strings.TrimPrefix(path, home+string(filepath.Separator))
		fmt.Fprintf(&b, "HARNESS MEMORY FILE %s modified=%s\n%s\n\n", rel, info.ModTime().UTC().Format(time.RFC3339), redactText(string(data)))
	}
	return b.String()
}

func resolveNoteSession(n inboxNote, listed []dreamSession) string {
	created, err := time.Parse(time.RFC3339, n.Created)
	if err != nil {
		return "unknown"
	}
	for _, s := range listed {
		started, err1 := time.Parse(time.RFC3339, s.StartedAt)
		ended, err2 := time.Parse(time.RFC3339, s.EndedAt)
		if err1 == nil && err2 == nil && s.Cwd == n.Cwd && !created.Before(started) && !created.After(ended) {
			return s.ID
		}
	}
	return "unknown"
}

func copyRedacted(src, dst string) error {
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(redactText(string(data))), 0o644)
}

// ---- running --------------------------------------------------------------

// runnerArgs is the only place a runner's permissions are set. An unknown runner
// is refused, so a misconfigured value can never start an unrestricted agent.
func runnerArgs(runner, prompt string) ([]string, error) {
	model := os.Getenv("REM_DREAM_MODEL")
	switch runner {
	case "claude":
		if model == "" {
			model = "haiku"
		}
		return []string{"claude", "-p", "--safe-mode", "--model", model,
			"--tools", "Read,Write,Edit,Glob,Grep", "--permission-mode", "acceptEdits",
			"--no-session-persistence", prompt}, nil
	}
	return nil, fmt.Errorf("REM_DREAM_RUNNER %q is not supported; use claude", runner)
}

func runRunner(dir, runner, key, prompt string) error {
	argv, err := runnerArgs(runner, prompt)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dreamRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	// A subscription login does the work; a key in the environment would bill the API.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	runErr := cmd.Run()
	logPath := filepath.Join(dir, ".dream", "logs", key+".log")
	if werr := os.WriteFile(logPath, output.Bytes(), 0o644); werr != nil {
		return werr
	}
	if runErr != nil {
		return fmt.Errorf("%s: %w (log %s)", argv[0], runErr, logPath)
	}
	return nil
}

func readDreamResult(path string) (dreamResult, error) {
	var result dreamResult
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("%s: %w", path, err)
	}
	return result, nil
}

// runAnalyzers runs one analyzer per input and retries each once. An input counts
// as read only when its result file exists and parses.
func runAnalyzers(memory, runner string, parallel int, inputs []dreamInput) (map[string]dreamResult, []string) {
	results := map[string]dreamResult{}
	var failed []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)
	for _, input := range inputs {
		wg.Add(1)
		go func(in dreamInput) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			resultPath := filepath.Join(memory, ".dream", "results", in.Key+".json")
			task := fmt.Sprintf("%s\nInput file: .dream/inputs/%s.txt\nResult file: .dream/results/%s.json\n", analyzerPrompt, in.Key, in.Key)
			for attempt := 1; attempt <= 2; attempt++ {
				err := runRunner(memory, runner, fmt.Sprintf("%s.%d", in.Key, attempt), task)
				result, readErr := readDreamResult(resultPath)
				if err == nil && readErr == nil {
					mu.Lock()
					results[in.Key] = result
					mu.Unlock()
					fmt.Printf("rem dream: read %s\n", in.Key)
					return
				}
				fmt.Fprintf(os.Stderr, "rem dream: %s attempt %d failed: %v %v\n", in.Key, attempt, err, readErr)
			}
			mu.Lock()
			failed = append(failed, in.Key)
			mu.Unlock()
		}(input)
	}
	wg.Wait()
	sort.Strings(failed)
	return results, failed
}

// ---- proposals and checks ---------------------------------------------------

func readProposals(path string) ([]dreamProposal, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []dreamProposal
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var p dreamProposal
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("%q: %w", truncate(line, 80), err)
		}
		out = append(out, p)
	}
	return out, nil
}

// editTopLayer applies add and remove proposals for one file. It returns the new
// text, the number applied, and the proposals it could not place.
func editTopLayer(text string, proposals []dreamProposal) (string, int, []dreamProposal) {
	lines := strings.Split(text, "\n")
	applied := 0
	var skipped []dreamProposal
	for _, p := range proposals {
		target := strings.TrimRight(p.Line, " \t")
		switch p.Action {
		case "remove":
			at := -1
			for i, line := range lines {
				if strings.TrimRight(line, " \t") == target {
					at = i
					break
				}
			}
			if at < 0 {
				skipped = append(skipped, p)
				continue
			}
			lines = append(lines[:at], lines[at+1:]...)
			applied++
		case "add":
			at := sectionEnd(lines, p.Section)
			if at < 0 || target == "" {
				skipped = append(skipped, p)
				continue
			}
			lines = append(lines[:at:at], append([]string{target}, lines[at:]...)...)
			applied++
		default:
			skipped = append(skipped, p)
		}
	}
	return strings.Join(lines, "\n"), applied, skipped
}

// sectionEnd finds where a new line belongs in a section: after its last non-blank line.
func sectionEnd(lines []string, heading string) int {
	heading = strings.TrimSpace(heading)
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 || heading == "" {
		return -1
	}
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		hashes := len(lines[i]) - len(strings.TrimLeft(lines[i], "#"))
		if hashes > 0 && hashes <= level && strings.HasPrefix(lines[i][hashes:], " ") {
			end = i
			break
		}
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

// applyProposals edits USER.md on the branch and writes AGENTS.md changes to a
// patch file, because AGENTS.md lives in another repo.
func applyProposals(worktree, agentsMD, date string, proposals []dreamProposal) (string, int, []dreamProposal, error) {
	var forUser, forAgents []dreamProposal
	var skipped []dreamProposal
	for _, p := range proposals {
		switch p.Target {
		case "USER.md":
			forUser = append(forUser, p)
		case "AGENTS.md":
			forAgents = append(forAgents, p)
		default:
			skipped = append(skipped, p)
		}
	}
	userPath := filepath.Join(worktree, "profile", "USER.md")
	applied := 0
	if len(forUser) > 0 {
		data, err := os.ReadFile(userPath)
		if err != nil {
			return "", 0, nil, err
		}
		text, n, miss := editTopLayer(string(data), forUser)
		skipped = append(skipped, miss...)
		if n > 0 {
			if err := os.WriteFile(userPath, []byte(text), 0o644); err != nil {
				return "", 0, nil, err
			}
			applied = n
		}
	}
	if len(forAgents) == 0 {
		return "", applied, skipped, nil
	}
	data, err := os.ReadFile(agentsMD)
	if err != nil {
		return "", 0, nil, err
	}
	text, n, miss := editTopLayer(string(data), forAgents)
	skipped = append(skipped, miss...)
	if n == 0 {
		return "", applied, skipped, nil
	}
	patch, err := unifiedPatch("AGENTS.md", string(data), text)
	if err != nil {
		return "", 0, nil, err
	}
	name := "rem-dream-v2-" + date + "-agents.patch"
	if err := os.MkdirAll(filepath.Join(worktree, "reviews"), 0o755); err != nil {
		return "", 0, nil, err
	}
	if err := os.WriteFile(filepath.Join(worktree, "reviews", name), []byte(patch), 0o644); err != nil {
		return "", 0, nil, err
	}
	return "reviews/" + name, applied, skipped, nil
}

// unifiedPatch diffs two versions in a throwaway repo so the patch has a/ and b/ paths.
func unifiedPatch(name, before, after string) (string, error) {
	dir, err := os.MkdirTemp("", "rem-dream-patch-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if out, err := gitIn(dir, "init", "-q"); err != nil {
		return "", fmt.Errorf("git init: %w (%s)", err, out)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		return "", err
	}
	if out, err := gitIn(dir, "add", name); err != nil {
		return "", fmt.Errorf("git add: %w (%s)", err, out)
	}
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		return "", err
	}
	out, err := gitIn(dir, "diff", name)
	if err != nil {
		return "", fmt.Errorf("git diff: %w (%s)", err, out)
	}
	return out, nil
}

func memoryPages(memory string) []string {
	var pages []string
	_ = filepath.WalkDir(memory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == "_inbox" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") && d.Name() != "_index.md" {
			pages = append(pages, path)
		}
		return nil
	})
	return pages
}

func checkMemoryPages(memory string) []string {
	var warnings []string
	if _, err := os.Stat(filepath.Join(memory, "_index.md")); err != nil {
		warnings = append(warnings, "`_index.md` is missing")
	}
	for _, path := range memoryPages(memory) {
		rel, _ := filepath.Rel(memory, path)
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, rel+": unreadable")
			continue
		}
		if len(data) > dreamPageCap {
			warnings = append(warnings, fmt.Sprintf("%s: %d bytes, over the page cap", rel, len(data)))
		}
		if strings.HasPrefix(strings.TrimSpace(string(data)), "MOVED TO") {
			continue
		}
		if !strings.HasPrefix(string(data), "---\n") {
			warnings = append(warnings, rel+": no frontmatter")
		}
	}
	return warnings
}

func memoryBytes(memory string) int {
	total := 0
	for _, path := range memoryPages(memory) {
		if info, err := os.Stat(path); err == nil {
			total += int(info.Size())
		}
	}
	return total
}

func renderDreamReport(date, branch string, selected []dreamSession, inputs []dreamInput, results map[string]dreamResult, organizer dreamResult, proposals, skipped []dreamProposal, warnings []string, memBytes, rendered int, agentsPatch string) string {
	var b strings.Builder
	facts, corrections, conflicts, cut := 0, 0, organizer.Conflicts, 0
	for _, in := range inputs {
		r := results[in.Key]
		facts += r.FactsAdded
		corrections += r.Corrected
		conflicts += r.Conflicts
		if in.Cut {
			cut++
		}
	}
	fmt.Fprintf(&b, "# rem dream v2 report %s\n\nBranch `%s`. Nothing here is live until it is merged.\n\n", date, branch)
	fmt.Fprintf(&b, "- inputs read: %d of %d expected (%d sessions)\n", len(results), len(inputs), len(selected))
	fmt.Fprintf(&b, "- inputs with shortened tool output: %d\n", cut)
	fmt.Fprintf(&b, "- facts added: %d\n- corrections: %d\n- conflicts: %d\n- pages merged: %d\n- index entries: %d\n", facts, corrections, conflicts, organizer.Merged, organizer.Entries)
	fmt.Fprintf(&b, "- memory pages: %d bytes, from %d bytes of rendered input\n\n", memBytes, rendered)
	if organizer.Summary != "" {
		fmt.Fprintf(&b, "## Organizer\n\n%s\n\n", organizer.Summary)
	}
	if len(warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
	if len(proposals) > 0 {
		b.WriteString("## Proposed changes to the top layer\n\n")
		for _, p := range proposals {
			fmt.Fprintf(&b, "- %s %s: `%s`. %s (%s)\n", p.Action, p.Target, p.Line, p.Why, strings.Join(p.Sources, ", "))
		}
		if agentsPatch != "" {
			fmt.Fprintf(&b, "\nAGENTS.md changes are in `%s`, for `git apply` in that repo.\n", agentsPatch)
		}
		b.WriteString("\n")
	}
	if len(skipped) > 0 {
		b.WriteString("## Proposals that could not be placed\n\n")
		for _, p := range skipped {
			fmt.Fprintf(&b, "- %s %s: `%s`\n", p.Action, p.Target, p.Line)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Inputs\n\n")
	for _, in := range inputs {
		fmt.Fprintf(&b, "### %s (%s)\n\n%s\n\n", in.Key, in.Label, strings.TrimSpace(results[in.Key].Summary))
	}
	return b.String()
}

// commitDream stages paths, refuses anything outside the dream's own output, and
// blocks on any credential in the added lines.
func commitDream(worktree, message string, paths ...string) error {
	if out, err := gitIn(worktree, append([]string{"add", "--"}, paths...)...); err != nil {
		return fmt.Errorf("git add: %w (%s)", err, out)
	}
	status, err := gitIn(worktree, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w (%s)", err, status)
	}
	for _, line := range strings.Split(strings.TrimRight(status, "\n"), "\n") {
		if len(line) < 3 {
			continue
		}
		if file := strings.Trim(line[3:], `"`); !allowedDreamPath(file) {
			return fmt.Errorf("refusing to commit: the dream changed %s; nothing committed, see %s", file, worktree)
		}
	}
	diff, err := gitIn(worktree, "diff", "--cached", "-U0")
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	file := ""
	var hits []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			file = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if strings.HasPrefix(line, "+") && hasSecret(line[1:]) {
			hits = append(hits, file)
		}
	}
	if len(hits) > 0 {
		return fmt.Errorf("secret scan blocked the commit, %d lines in: %s; nothing committed, see %s", len(hits), strings.Join(hits, ", "), worktree)
	}
	if out, err := gitIn(worktree, "commit", "-q", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w (%s)", err, out)
	}
	return nil
}

func allowedDreamPath(file string) bool {
	return strings.HasPrefix(file, "memory/") || strings.HasPrefix(file, "reviews/rem-dream-v2-") || file == "profile/USER.md"
}
