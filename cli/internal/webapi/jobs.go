package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/config"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

const (
	jobRunning  = "running"
	jobDone     = "done"
	jobFailed   = "failed"
	jobCanceled = "canceled"

	maxJobEvents = 500
)

var errJobRunning = errors.New("another extraction job is already running")

type jobManager struct {
	mu      sync.Mutex
	current *jobState
	last    *jobState
}

type jobState struct {
	snapshot JobSnapshot
	events   []JobEvent
	subs     map[chan JobEvent]struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	nextSeq  int
}

func (m *jobManager) start(run func(context.Context, func(pipeline.ProgressEvent)) (*pipeline.RunResult, error)) (JobSnapshot, error) {
	m.mu.Lock()
	if m.current != nil && m.current.snapshot.Status == jobRunning {
		m.mu.Unlock()
		return JobSnapshot{}, errJobRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &jobState{
		snapshot: JobSnapshot{
			ID:        newJobID(),
			Status:    jobRunning,
			StartedAt: time.Now().UTC(),
		},
		subs:   map[chan JobEvent]struct{}{},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.current = job
	snapshot := job.snapshot
	m.mu.Unlock()

	go func() {
		started := time.Now()
		result, err := run(ctx, func(event pipeline.ProgressEvent) {
			m.publish(job, event)
		})
		m.finish(job, result, err, ctx.Err(), time.Since(started))
	}()
	return snapshot, nil
}

func (m *jobManager) publish(job *jobState, event pipeline.ProgressEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job.snapshot.Status != jobRunning {
		return
	}
	switch event.Phase {
	case "selected":
		job.snapshot.Total = event.Total
	case "segment":
		job.snapshot.Total = event.Total
		job.snapshot.Done = event.Done
		switch {
		case event.Cached:
			job.snapshot.Cached++
		case event.Failed:
			job.snapshot.Failed++
		default:
			job.snapshot.Extracted++
		}
	}
	record := JobEvent{ProgressEvent: event, Seq: job.nextSeq, At: time.Now().UTC()}
	job.nextSeq++
	job.events = appendBounded(job.events, record)
	broadcastLocked(job, record)
}

func (m *jobManager) finish(job *jobState, result *pipeline.RunResult, runErr, ctxErr error, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	finished := time.Now().UTC()
	status := jobDone
	switch {
	case ctxErr != nil:
		status = jobCanceled
	case result == nil:
		status = jobFailed
	}
	job.snapshot.Status = status
	job.snapshot.FinishedAt = &finished
	if result != nil {
		job.snapshot.Total = result.Selected
		job.snapshot.Done = processedReports(result.Reports)
		job.snapshot.Cached = result.CacheHits
		job.snapshot.Extracted = result.Extracted
		job.snapshot.Failed = result.FailedSegments
		job.snapshot.Result = buildJobResult(result, elapsed)
	}
	switch {
	case ctxErr != nil:
		job.snapshot.Error = "操作已取消；已完成的文档会保留"
	case runErr != nil:
		job.snapshot.Error = runErr.Error()
	}
	final := JobEvent{
		ProgressEvent: pipeline.ProgressEvent{
			Phase: status,
			Total: job.snapshot.Total,
			Done:  job.snapshot.Done,
			Error: job.snapshot.Error,
		},
		Seq: job.nextSeq,
		At:  finished,
	}
	job.nextSeq++
	job.events = appendBounded(job.events, final)
	broadcastLocked(job, final)

	job.cancel = nil
	close(job.done)
	m.last = job
	m.current = nil
}

func (m *jobManager) findByID(id string) *jobState {
	if m.current != nil && m.current.snapshot.ID == id {
		return m.current
	}
	if m.last != nil && m.last.snapshot.ID == id {
		return m.last
	}
	return nil
}

func (m *jobManager) snapshot(id string, withEvents bool) (JobSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.findByID(id)
	if job == nil {
		return JobSnapshot{}, false
	}
	return copySnapshot(job, withEvents), true
}

func (m *jobManager) latest(withEvents bool) *JobSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.current
	if job == nil {
		job = m.last
	}
	if job == nil {
		return nil
	}
	snapshot := copySnapshot(job, withEvents)
	return &snapshot
}

func (m *jobManager) cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && m.current.snapshot.ID == id && m.current.cancel != nil {
		m.current.cancel()
		return true
	}
	return false
}

func (m *jobManager) cancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && m.current.snapshot.Status == jobRunning && m.current.cancel != nil {
		m.current.cancel()
	}
}

func (m *jobManager) wait(ctx context.Context) error {
	m.mu.Lock()
	if m.current == nil {
		m.mu.Unlock()
		return nil
	}
	done := m.current.done
	m.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *jobManager) subscribe(id string) ([]JobEvent, chan JobEvent, func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.findByID(id)
	if job == nil {
		return nil, nil, nil, false
	}
	history := append([]JobEvent(nil), job.events...)
	ch := make(chan JobEvent, 128)
	job.subs[ch] = struct{}{}
	cleanup := func() {
		m.mu.Lock()
		delete(job.subs, ch)
		m.mu.Unlock()
	}
	return history, ch, cleanup, true
}

func copySnapshot(job *jobState, withEvents bool) JobSnapshot {
	snapshot := job.snapshot
	if withEvents {
		snapshot.Events = append([]JobEvent(nil), job.events...)
	}
	return snapshot
}

func appendBounded(events []JobEvent, event JobEvent) []JobEvent {
	events = append(events, event)
	if len(events) > maxJobEvents {
		events = events[len(events)-maxJobEvents:]
	}
	return events
}

func broadcastLocked(job *jobState, event JobEvent) {
	for ch := range job.subs {
		select {
		case ch <- event:
		default:
			// A slow browser must not block extraction.
		}
	}
}

func buildJobResult(result *pipeline.RunResult, elapsed time.Duration) *JobResult {
	if result == nil {
		return nil
	}
	reports := make([]JobSegment, 0, len(result.Reports))
	for _, report := range result.Reports {
		if report.Segment.ID == "" {
			continue
		}
		item := JobSegment{
			SegmentID:  report.Segment.ID,
			Title:      report.Segment.Title,
			Project:    report.Segment.Project,
			Source:     report.Segment.Source,
			Cached:     report.Cached,
			DurationMS: report.Duration.Milliseconds(),
		}
		if report.Err != nil {
			item.Error = report.Err.Error()
		}
		if report.Doc != nil {
			item.Confidence = report.Doc.Confidence
			item.EvidenceChecked = report.Doc.Validation.EvidenceChecked
			item.EvidenceVerified = report.Doc.Validation.EvidenceVerified
		}
		reports = append(reports, item)
	}
	return &JobResult{
		Files:          result.Files,
		Parsed:         result.Parsed,
		Skipped:        result.Skipped,
		FailedFiles:    result.FailedFiles,
		Segments:       result.Segments,
		Selected:       result.Selected,
		CacheHits:      result.CacheHits,
		Extracted:      result.Extracted,
		FailedSegments: result.FailedSegments,
		Reports:        reports,
		Warnings:       result.Warnings,
		DurationMS:     elapsed.Milliseconds(),
	}
}

func processedReports(reports []pipeline.RunReport) int {
	done := 0
	for _, report := range reports {
		if report.Segment.ID != "" {
			done++
		}
	}
	return done
}

func newJobID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ---------- HTTP handlers ----------

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	provider, err := config.ValidateProvider(defaultString(query.Get("provider"), "all"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	since := defaultString(query.Get("since"), "7d")
	cutoff, err := config.ParseSince(since, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := positiveInt(query.Get("limit"), 0)

	prepared, prepareErr := pipeline.Prepare(r.Context(), pipeline.PrepareOptions{
		Load: pipeline.LoadOptions{
			Provider:         provider,
			ClaudeDir:        s.opts.ClaudeDir,
			CodexDir:         s.opts.CodexDir,
			Since:            cutoff,
			IncludeSubagents: query.Get("include_subagents") == "1",
			Limit:            limit,
		},
		Segment: segment.DefaultOptions(),
		Redact:  redact.DefaultConfig(),
	})
	if prepared == nil {
		if prepareErr == nil {
			prepareErr = errors.New("no sessions were prepared")
		}
		writeError(w, http.StatusInternalServerError, prepareErr.Error())
		return
	}

	infos, cacheWarnings := s.store.ListDocs()
	extracted := make(map[string]bool, len(infos))
	for _, info := range infos {
		if info.Doc != nil {
			extracted[info.ID] = true
		}
	}
	list := pipeline.BuildSessionList(prepared, extracted, time.Now(), since)
	if list.Sessions == nil {
		list.Sessions = []pipeline.SelectableSession{}
	}
	list.Warnings = append(list.Warnings, cacheWarnings...)
	if prepareErr != nil {
		list.Warnings = append(list.Warnings, prepareErr.Error())
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleStartExtract(w http.ResponseWriter, r *http.Request) {
	var request ExtractJobRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	options, err := s.extractOptions(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lock, err := cache.TryLock(s.opts.CacheDir)
	if err != nil {
		if errors.Is(err, cache.ErrLocked) {
			writeError(w, http.StatusConflict, err.Error()+"; retry after the active writer finishes")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	options.CacheLock = lock
	snapshot, err := s.jobs.start(func(ctx context.Context, onProgress func(pipeline.ProgressEvent)) (*pipeline.RunResult, error) {
		defer lock.Close()
		options.OnProgress = onProgress
		return pipeline.Run(ctx, options)
	})
	if err != nil {
		_ = lock.Close()
	}
	if errors.Is(err, errJobRunning) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": snapshot})
}

func (s *Server) handleCurrentJob(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"job": s.jobs.latest(true)})
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := s.jobs.snapshot(strings.TrimSpace(r.PathValue("id")), true)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": snapshot})
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	history, channel, cleanup, ok := s.jobs.subscribe(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job id")
		return
	}
	defer cleanup()
	if after := positiveInt(r.URL.Query().Get("after"), 0); after > 0 {
		filtered := make([]JobEvent, 0, len(history))
		for _, event := range history {
			if event.Seq >= after {
				filtered = append(filtered, event)
			}
		}
		history = filtered
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(event JobEvent) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return true
		}
		flusher.Flush()
		return event.Phase == jobDone || event.Phase == jobFailed || event.Phase == jobCanceled
	}
	for _, event := range history {
		if send(event) {
			return
		}
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.shutdown:
			return
		case event := <-channel:
			if send(event) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !s.jobs.cancel(id) {
		if _, ok := s.jobs.snapshot(id, false); !ok {
			writeError(w, http.StatusNotFound, "unknown job id")
			return
		}
		writeError(w, http.StatusConflict, "job is not running")
		return
	}
	snapshot, _ := s.jobs.snapshot(id, true)
	writeJSON(w, http.StatusOK, map[string]any{"job": snapshot})
}

func (s *Server) extractOptions(request ExtractJobRequest) (pipeline.RunOptions, error) {
	provider, err := config.ValidateProvider(defaultString(request.Provider, "all"))
	if err != nil {
		return pipeline.RunOptions{}, err
	}
	sessions := cleanStrings(request.Sessions)
	segments := cleanStrings(request.Segments)
	if len(sessions) == 0 && len(segments) == 0 {
		return pipeline.RunOptions{}, errors.New("select at least one session or segment")
	}
	since := defaultString(request.Since, "7d")
	cutoff, err := config.ParseSince(since, time.Now())
	if err != nil {
		return pipeline.RunOptions{}, err
	}
	concurrency := request.Concurrency
	if concurrency == 0 {
		concurrency = 2
	}
	if concurrency < 1 || concurrency > 8 {
		return pipeline.RunOptions{}, errors.New("concurrency must be between 1 and 8")
	}
	timeoutSeconds := request.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 180
	}
	if timeoutSeconds < 30 || timeoutSeconds > 900 {
		return pipeline.RunOptions{}, errors.New("timeout_seconds must be between 30 and 900")
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	return pipeline.RunOptions{
		Load: pipeline.LoadOptions{
			Provider:         provider,
			ClaudeDir:        s.opts.ClaudeDir,
			CodexDir:         s.opts.CodexDir,
			Since:            cutoff,
			IncludeSubagents: request.IncludeSubagents,
		},
		Segment:     segment.DefaultOptions(),
		Redact:      redact.DefaultConfig(),
		Build:       extract.DefaultBuildOptions(),
		CacheDir:    s.opts.CacheDir,
		CursorPath:  s.opts.CursorPath,
		Full:        request.Full,
		Force:       request.Force,
		Concurrency: concurrency,
		Segments:    0,
		Selection:   pipeline.Selection{Sessions: sessions, Segments: segments},
		ClientFor:   s.clientFactory(provider, strings.TrimSpace(request.Model), timeout),
		Logger:      discardLogger(),
		Now:         time.Now,
	}, nil
}

func (s *Server) clientFactory(provider, model string, timeout time.Duration) pipeline.ClientFor {
	if s.opts.ClientFor != nil {
		return s.opts.ClientFor
	}
	clients := map[string]extract.Client{}
	var mu sync.Mutex
	return func(_ context.Context, source string) (extract.Client, error) {
		name := strings.TrimSpace(source)
		if name == "" {
			name = provider
		}
		mu.Lock()
		if client, ok := clients[name]; ok {
			mu.Unlock()
			return client, nil
		}
		mu.Unlock()

		bin := ""
		switch name {
		case "claude-code":
			bin = s.opts.ClaudeBin
		case "codex":
			bin = s.opts.CodexBin
		default:
			return nil, fmt.Errorf("unsupported segment source %q", name)
		}
		client, err := extract.NewCLIClient(extract.CLIOptions{
			Provider: name,
			Bin:      bin,
			Model:    model,
			Timeout:  timeout,
		})
		if err != nil {
			return nil, err
		}
		mu.Lock()
		clients[name] = client
		mu.Unlock()
		return client, nil
	}
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
