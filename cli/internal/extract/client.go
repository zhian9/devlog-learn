package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Request is one model invocation.
type Request struct {
	Prompt     string
	SchemaFile string
}

// Response contains separated streams and the final model text.
type Response struct {
	Text      string
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
	Duration  time.Duration
	Usage     Usage
}

// Client is the LLM boundary. Tests use a fake implementation.
type Client interface {
	Name() string
	Version(ctx context.Context) (string, error)
	Capabilities(ctx context.Context) (map[string]bool, error)
	Complete(ctx context.Context, req Request) (Response, error)
}

// CLIOptions configures the local claude/codex subprocess client.
type CLIOptions struct {
	Provider  string
	Bin       string
	Model     string
	WorkDir   string
	Timeout   time.Duration
	MaxStdout int
	MaxStderr int
	ExtraArgs []string
}

// CLIClient invokes the locally installed Claude Code or Codex CLI.
type CLIClient struct {
	provider  string
	bin       string
	model     string
	workDir   string
	timeout   time.Duration
	maxStdout int
	maxStderr int
	extraArgs []string

	mu             sync.Mutex
	version        string
	versionDone    bool
	caps           map[string]bool
	capsDone       bool
	effectiveModel string
}

func NewCLIClient(opt CLIOptions) (*CLIClient, error) {
	provider := strings.ToLower(strings.TrimSpace(opt.Provider))
	switch provider {
	case "claude-code", "codex":
	default:
		return nil, fmt.Errorf("unsupported LLM provider %q: supported providers are claude-code and codex", opt.Provider)
	}

	bin := strings.TrimSpace(opt.Bin)
	if bin == "" {
		name := "claude"
		if provider == "codex" {
			name = "codex"
		}
		bin = name
	}
	resolved, err := resolveExecutable(bin)
	if err != nil {
		return nil, fmt.Errorf("resolve %s executable %q: %w", provider, bin, err)
	}

	if opt.Timeout <= 0 {
		opt.Timeout = 180 * time.Second
	}
	if opt.MaxStdout <= 0 {
		opt.MaxStdout = 4 << 20
	}
	if opt.MaxStderr <= 0 {
		opt.MaxStderr = 256 << 10
	}
	if opt.WorkDir != "" {
		info, statErr := os.Stat(opt.WorkDir)
		if statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("LLM work directory %q is not accessible", opt.WorkDir)
		}
	}

	return &CLIClient{
		provider:  provider,
		bin:       resolved,
		model:     strings.TrimSpace(opt.Model),
		workDir:   strings.TrimSpace(opt.WorkDir),
		timeout:   opt.Timeout,
		maxStdout: opt.MaxStdout,
		maxStderr: opt.MaxStderr,
		extraArgs: append([]string(nil), opt.ExtraArgs...),
	}, nil
}

func (c *CLIClient) Name() string { return c.provider }

func (c *CLIClient) Version(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.versionDone {
		version := c.version
		c.mu.Unlock()
		if version == "" {
			return "", errors.New("CLI version unavailable")
		}
		return version, nil
	}
	c.mu.Unlock()

	out, err := c.runProbe(ctx, []string{"--version"})
	if err != nil && strings.TrimSpace(out) == "" {
		return "", err
	}
	version := firstLine(out)
	c.mu.Lock()
	c.version = version
	c.versionDone = true
	c.mu.Unlock()
	if version == "" {
		return "", errors.New("CLI version output was empty")
	}
	return version, nil
}

func (c *CLIClient) Capabilities(ctx context.Context) (map[string]bool, error) {
	c.mu.Lock()
	if c.capsDone {
		caps := cloneCaps(c.caps)
		c.mu.Unlock()
		return caps, nil
	}
	c.mu.Unlock()

	args := []string{"--help"}
	if c.provider == "codex" {
		args = []string{"exec", "--help"}
	}
	out, err := c.runProbe(ctx, args)
	caps := detectCapabilities(out)
	if err != nil && out == "" {
		c.mu.Lock()
		c.caps = map[string]bool{}
		c.capsDone = true
		c.mu.Unlock()
		return map[string]bool{}, err
	}

	c.mu.Lock()
	c.caps = caps
	c.capsDone = true
	c.mu.Unlock()
	return cloneCaps(caps), nil
}

// Complete runs the CLI with the prompt on stdin. Output is capped and the
// provider-specific envelope/result file is parsed into Response.Text.
func (c *CLIClient) Complete(ctx context.Context, req Request) (Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	scratch := c.workDir
	if scratch == "" {
		dir, err := os.MkdirTemp("", "devlog-llm-*")
		if err != nil {
			return Response{}, fmt.Errorf("create LLM scratch directory: %w", err)
		}
		scratch = dir
		defer os.RemoveAll(dir)
	}

	caps, _ := c.Capabilities(ctx)
	lastMessagePath := filepath.Join(scratch, "last-message.txt")
	args := c.buildArgs(caps, lastMessagePath)

	cmd, err := newCommand(callCtx, c.bin, args, scratch)
	if err != nil {
		return Response{}, err
	}
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.WaitDelay = 2 * time.Second
	stdout := &cappedBuffer{limit: c.maxStdout}
	stderr := &cappedBuffer{limit: c.maxStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	runErr := cmd.Run()
	response := Response{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: stdout.Truncated() || stderr.Truncated(),
		Duration:  time.Since(started),
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			response.ExitCode = exitErr.ExitCode()
		} else {
			response.ExitCode = -1
		}
		if ctxErr := callCtx.Err(); ctxErr != nil {
			return response, fmt.Errorf("%s timed out or was canceled: %w", c.provider, ctxErr)
		}
		return response, fmt.Errorf("%s exited with code %d: %s", c.provider, response.ExitCode, tailRunes(response.Stderr, 500))
	}

	switch c.provider {
	case "claude-code":
		text, model, usage, err := parseClaudeOutput(response.Stdout)
		if err != nil {
			return response, err
		}
		response.Text = text
		response.Usage = usage
		if model != "" {
			c.setModel(model)
		}
	case "codex":
		text := ""
		if enabled(caps, "--output-last-message") {
			text = readTextFile(lastMessagePath, c.maxStdout)
		}
		if strings.TrimSpace(text) == "" {
			text = response.Stdout
		}
		response.Text = text
		response.Usage = parseCodexUsage(response.Stdout, response.Stderr)
	}

	response.Text = strings.TrimSpace(response.Text)
	if response.Text == "" {
		return response, fmt.Errorf("%s returned an empty response (stdout truncated=%v)", c.provider, stdout.Truncated())
	}
	return response, nil
}

func (c *CLIClient) setModel(model string) {
	c.mu.Lock()
	c.effectiveModel = model
	c.mu.Unlock()
}

// Model returns the effective model name for provenance.
func (c *CLIClient) Model() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.effectiveModel != "" {
		return c.effectiveModel
	}
	return c.model
}

func (c *CLIClient) buildArgs(caps map[string]bool, lastMessagePath string) []string {
	if c.provider == "claude-code" {
		args := []string{"-p"}
		if enabled(caps, "--output-format") {
			args = append(args, "--output-format", "json")
		}
		if enabled(caps, "--no-session-persistence") {
			args = append(args, "--no-session-persistence")
		}
		if c.model != "" && enabled(caps, "--model") {
			args = append(args, "--model", c.model)
		}
		return append(args, c.extraArgs...)
	}

	args := []string{"exec", "-"}
	if enabled(caps, "--skip-git-repo-check") {
		args = append(args, "--skip-git-repo-check")
	}
	if enabled(caps, "--ephemeral") {
		args = append(args, "--ephemeral")
	}
	if enabled(caps, "--sandbox") {
		args = append(args, "--sandbox", "read-only")
	}
	if enabled(caps, "--color") {
		args = append(args, "--color", "never")
	}
	if c.model != "" && enabled(caps, "--model") {
		args = append(args, "--model", c.model)
	}
	if enabled(caps, "--output-last-message") {
		args = append(args, "--output-last-message", lastMessagePath)
	}
	return append(args, c.extraArgs...)
}

func (c *CLIClient) runProbe(ctx context.Context, args []string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd, err := newCommand(probeCtx, c.bin, args, c.workDir)
	if err != nil {
		return "", err
	}
	cmd.WaitDelay = 2 * time.Second
	stdout := &cappedBuffer{limit: 1 << 20}
	stderr := &cappedBuffer{limit: 1 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	out := stdout.String()
	if out == "" {
		out = stderr.String()
	}
	if err != nil {
		if ctxErr := probeCtx.Err(); ctxErr != nil {
			return out, fmt.Errorf("%s probe timed out: %w", c.provider, ctxErr)
		}
		return out, fmt.Errorf("%s probe failed: %w", c.provider, err)
	}
	return out, nil
}

type claudeEnvelope struct {
	Type         string                      `json:"type"`
	Subtype      string                      `json:"subtype"`
	Result       string                      `json:"result"`
	IsError      bool                        `json:"is_error"`
	Usage        claudeUsage                 `json:"usage"`
	TotalCostUSD float64                     `json:"total_cost_usd"`
	ModelUsage   map[string]claudeModelUsage `json:"modelUsage"`
}

func parseClaudeOutput(stdout string) (string, string, Usage, error) {
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") {
		return stdout, "", Usage{}, nil
	}
	var env claudeEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return stdout, "", Usage{}, nil
	}
	// Raw model JSON is also an object. Only treat it as an envelope when the
	// CLI actually produced a result envelope.
	if env.Type != "result" && strings.TrimSpace(env.Result) == "" {
		return stdout, "", Usage{}, nil
	}
	usage := usageFromClaude(env)
	if env.IsError {
		return "", "", usage, fmt.Errorf("claude reported an error: %s", firstNonEmpty(env.Subtype, env.Type))
	}
	model := ""
	if len(env.ModelUsage) > 0 {
		names := make([]string, 0, len(env.ModelUsage))
		for name := range env.ModelUsage {
			names = append(names, name)
		}
		sort.Strings(names)
		model = names[0]
	}
	if strings.TrimSpace(env.Result) == "" {
		return "", model, usage, fmt.Errorf("claude envelope has no result")
	}
	return env.Result, model, usage, nil
}

func detectCapabilities(help string) map[string]bool {
	flags := []string{
		"--output-format",
		"--no-session-persistence",
		"--model",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox",
		"--color",
		"--output-last-message",
		"--output-schema",
	}
	out := make(map[string]bool, len(flags))
	for _, flag := range flags {
		out[flag] = strings.Contains(help, flag)
	}
	return out
}

// enabled treats an empty capability map as "use the known-good flags for the
// installed CLI". A successful probe yields a non-empty map and is authoritative.
func enabled(caps map[string]bool, flag string) bool {
	if len(caps) == 0 {
		return true
	}
	return caps[flag]
}

func cloneCaps(caps map[string]bool) map[string]bool {
	out := make(map[string]bool, len(caps))
	for key, value := range caps {
		out[key] = value
	}
	return out
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func tailRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return "..." + string(runes[len(runes)-limit:])
}

func readTextFile(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if limit > 0 && len(data) > limit {
		data = data[:limit]
	}
	return strings.TrimSpace(string(data))
}
