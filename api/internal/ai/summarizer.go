package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	DefaultModel   = "claude-opus-5"
	requestTimeout = 45 * time.Second
	maxErrorChars  = 4000
	maxPayloadCars = 1500
	maxLogLines    = 40
)

var (
	ErrDisabled = errors.New("ai: failure summaries are not configured")
	ErrUpstream = errors.New("ai: model request failed")
	ErrRefused  = errors.New("ai: model declined to answer")
)

type ExecutionSample struct {
	AttemptNumber int
	Status        string
	DurationMs    *int
	ErrorMessage  string
	StartedAt     time.Time
}

type LogSample struct {
	Level    string
	Message  string
	LoggedAt time.Time
}

type FailureContext struct {
	JobID        string
	JobType      string
	QueueName    string
	Status       string
	AttemptCount int
	MaxAttempts  int
	TimeoutSecs  int
	LastError    string
	Payload      string
	Executions   []ExecutionSample
	Logs         []LogSample
}

type Summary struct {
	Summary         string `json:"summary"`
	LikelyCause     string `json:"likely_cause"`
	SuggestedAction string `json:"suggested_action"`
	Category        string `json:"category"`
	Confidence      string `json:"confidence"`
	IsTransient     bool   `json:"is_transient"`
	Model           string `json:"-"`
	InputTokens     int    `json:"-"`
	OutputTokens    int    `json:"-"`
}

var categories = []string{
	"timeout", "dependency_failure", "invalid_payload", "permission",
	"rate_limit", "resource_exhaustion", "logic_error", "infrastructure", "unknown",
}

const systemPrompt = `You triage failed background jobs for a distributed job scheduler.

You receive one job's failure context: its type, retry state, terminal error, a truncated payload, its per-attempt execution records, and its execution logs.

Produce a diagnosis an on-call engineer can act on immediately:
- summary: one sentence, plain language, what actually went wrong. No restating the raw error verbatim.
- likely_cause: the most probable root cause given the evidence. If the evidence is thin, say so rather than inventing specifics.
- suggested_action: the concrete next step. Prefer "retry after X" / "fix Y in the payload" / "check Z dependency" over generic advice.
- category: the closest match from the allowed set.
- confidence: how well the evidence supports your diagnosis.
- is_transient: true only when a plain retry has a realistic chance of succeeding with no other change.

Never invent stack frames, service names, or configuration values that do not appear in the evidence. The payload may contain user data; describe it, never echo secrets.`

func schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary":          map[string]any{"type": "string", "maxLength": 300},
			"likely_cause":     map[string]any{"type": "string", "maxLength": 500},
			"suggested_action": map[string]any{"type": "string", "maxLength": 500},
			"category":         map[string]any{"type": "string", "enum": categories},
			"confidence":       map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"is_transient":     map[string]any{"type": "boolean"},
		},
		"required": []string{
			"summary", "likely_cause", "suggested_action", "category", "confidence", "is_transient",
		},
		"additionalProperties": false,
	}
}

type Summarizer struct {
	client  anthropic.Client
	model   string
	enabled bool
}

func New(apiKey, model string) *Summarizer {
	if strings.TrimSpace(apiKey) == "" {
		return &Summarizer{}
	}
	if model == "" {
		model = DefaultModel
	}
	return &Summarizer{
		client:  anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:   model,
		enabled: true,
	}
}

func (s *Summarizer) Enabled() bool { return s != nil && s.enabled }

func (s *Summarizer) Model() string {
	if s == nil || s.model == "" {
		return DefaultModel
	}
	return s.model
}

func (s *Summarizer) Summarize(ctx context.Context, fc FailureContext) (*Summary, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(s.model),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: schema()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fc.Render())),
		},
	})
	if err != nil {
		return nil, classify(err)
	}

	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("%w: %s", ErrRefused, resp.StopDetails.Explanation)
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	if text.Len() == 0 {
		return nil, fmt.Errorf("%w: model returned no content", ErrUpstream)
	}

	var out Summary
	if err := json.Unmarshal([]byte(text.String()), &out); err != nil {
		return nil, fmt.Errorf("%w: response was not valid json: %v", ErrUpstream, err)
	}
	if out.Summary == "" || out.Category == "" || out.Confidence == "" {
		return nil, fmt.Errorf("%w: response was missing required fields", ErrUpstream)
	}

	out.Model = s.model
	out.InputTokens = int(resp.Usage.InputTokens)
	out.OutputTokens = int(resp.Usage.OutputTokens)
	return &out, nil
}

func classify(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	switch apiErr.StatusCode {
	case 401, 403:
		return fmt.Errorf("%w: credentials rejected by the model provider", ErrUpstream)
	case 429:
		return fmt.Errorf("%w: model provider rate limit reached, retry shortly", ErrUpstream)
	case 529:
		return fmt.Errorf("%w: model provider is overloaded, retry shortly", ErrUpstream)
	default:
		return fmt.Errorf("%w: provider returned %d (request %s)", ErrUpstream, apiErr.StatusCode, apiErr.RequestID)
	}
}

func (fc FailureContext) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "job_type: %s\n", fc.JobType)
	fmt.Fprintf(&b, "queue: %s\n", fc.QueueName)
	fmt.Fprintf(&b, "final_status: %s\n", fc.Status)
	fmt.Fprintf(&b, "attempts: %d of %d\n", fc.AttemptCount, fc.MaxAttempts)
	fmt.Fprintf(&b, "timeout_secs: %d\n", fc.TimeoutSecs)

	b.WriteString("\nterminal_error:\n")
	b.WriteString(truncate(orNone(fc.LastError), maxErrorChars))

	b.WriteString("\n\npayload (truncated):\n")
	b.WriteString(truncate(orNone(fc.Payload), maxPayloadCars))

	b.WriteString("\n\nattempts:\n")
	if len(fc.Executions) == 0 {
		b.WriteString("  none recorded\n")
	}
	for _, e := range fc.Executions {
		duration := "n/a"
		if e.DurationMs != nil {
			duration = fmt.Sprintf("%dms", *e.DurationMs)
		}
		fmt.Fprintf(&b, "  #%d status=%s duration=%s error=%s\n",
			e.AttemptNumber, e.Status, duration, truncate(orNone(e.ErrorMessage), 400))
	}

	b.WriteString("\nlogs:\n")
	if len(fc.Logs) == 0 {
		b.WriteString("  none recorded\n")
	}
	for i, l := range fc.Logs {
		if i >= maxLogLines {
			fmt.Fprintf(&b, "  ... %d earlier lines omitted\n", len(fc.Logs)-maxLogLines)
			break
		}
		fmt.Fprintf(&b, "  [%s] %s\n", l.Level, truncate(l.Message, 400))
	}

	return b.String()
}

func (fc FailureContext) Fingerprint(model string) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|%s|", model, fc.JobType)
	h.Write([]byte(fc.Render()))
	return hex.EncodeToString(h.Sum(nil))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n... truncated, %d more characters", len(s)-max)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none recorded)"
	}
	return s
}
