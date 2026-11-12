package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"devlog-learn/internal/version"
	"devlog-learn/internal/webapi"
)

type serveOptions struct {
	addr      string
	cacheDir  string
	cursorRef string
	outDir    string
	claudeDir string
	codexDir  string
	claudeBin string
	codexBin  string
	noOpen    bool
}

func newServeCmd() *cobra.Command {
	opt := &serveOptions{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Open the local web UI for reading and reviewing project memory",
		Long: `Start the local web UI on 127.0.0.1 and open it in an application window.

The server is loopback-only, uses a per-process access token, embeds the
frontend into this binary, and never calls a model. Read-only browsing is
available immediately; review and question changes write only the same cache
files the CLI already writes.

The UI can be closed and the command stopped with Ctrl+C. Nothing leaves the
machine.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, opt)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opt.addr, "addr", "127.0.0.1:8787", "loopback listen address; use 127.0.0.1:0 for an ephemeral port")
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	flags.StringVar(&opt.cursorRef, "cursor", ".devlog-cursor.json", "cursor file to inspect")
	flags.StringVar(&opt.outDir, "out", ".devlog-docs", "rendered Markdown/index output directory")
	flags.StringVar(&opt.claudeDir, "claude-dir", "", "Claude Code projects directory (default ~/.claude/projects)")
	flags.StringVar(&opt.codexDir, "codex-dir", "", "Codex sessions directory (default ~/.codex/sessions)")
	flags.StringVar(&opt.claudeBin, "claude-bin", "claude", "Claude Code executable shown in the health panel")
	flags.StringVar(&opt.codexBin, "codex-bin", "codex", "Codex executable shown in the health panel")
	flags.BoolVar(&opt.noOpen, "no-open", false, "do not open a browser window automatically")
	return cmd
}

func runServe(cmd *cobra.Command, opt *serveOptions) error {
	server, err := webapi.New(webapi.Options{
		Addr:       opt.addr,
		CacheDir:   opt.cacheDir,
		CursorPath: opt.cursorRef,
		OutDir:     opt.outDir,
		ClaudeDir:  opt.claudeDir,
		CodexDir:   opt.codexDir,
		ClaudeBin:  opt.claudeBin,
		CodexBin:   opt.codexBin,
	})
	if err != nil {
		return err
	}
	listener, err := server.Listen()
	if err != nil {
		return fmt.Errorf("listen on %s: %w (try --addr 127.0.0.1:0)", opt.addr, err)
	}
	url := server.URL(listener.Addr())

	stdout := cmd.OutOrStdout()
	fmt.Fprintf(stdout, "devlog-learn %s\n", version.ToolVersion)
	fmt.Fprintf(stdout, "web UI   %s\n", url)
	fmt.Fprintf(stdout, "cache    %s\n", opt.cacheDir)
	fmt.Fprintf(stdout, "output   %s\n", opt.outDir)
	fmt.Fprintln(stdout, "keep this process running; press Ctrl+C to stop")

	if !opt.noOpen {
		go openUI(url)
	}
	return server.Serve(cmd.Context(), listener)
}

// openUI prefers a chromeless application window when Edge or Chrome is
// installed, because that is the simplest desktop experience without adding a
// desktop framework or installer. It falls back to the default browser.
func openUI(url string) {
	switch runtime.GOOS {
	case "windows":
		for _, candidate := range browserCandidates() {
			if _, err := os.Stat(candidate); err != nil {
				continue
			}
			process := exec.Command(candidate,
				"--app="+url,
				"--window-size=1440,960",
				"--window-position=80,40",
				"--no-first-run",
			)
			if err := process.Start(); err == nil {
				_ = process.Wait()
				return
			}
		}
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}

func browserCandidates() []string {
	var out []string
	for _, root := range []string{
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("ProgramFiles"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if root == "" {
			continue
		}
		out = append(out,
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
		)
	}
	return out
}
