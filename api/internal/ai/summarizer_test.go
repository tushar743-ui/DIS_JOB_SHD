package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func sampleContext() FailureContext {
	duration := 30412
	return FailureContext{
		JobID:        "11111111-2222-3333-4444-555555555555",
		JobType:      "process_payment",
		QueueName:    "payments",
		Status:       "dead",
		AttemptCount: 3,
		MaxAttempts:  3,
		TimeoutSecs:  30,
		LastError:    "context deadline exceeded calling billing-gateway",
		Payload:      `{"order_id":"A-901","amount_cents":4200}`,
		Executions: []ExecutionSample{
			{AttemptNumber: 1, Status: "failed", DurationMs: &duration,
				ErrorMessage: "context deadline exceeded", StartedAt: time.Now()},
		},
		Logs: []LogSample{
			{Level: "error", Message: "billing-gateway did not respond", LoggedAt: time.Now()},
		},
	}
}

func stubServer(t *testing.T, handler http.HandlerFunc) *Summarizer {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Summarizer{
		client: anthropic.NewClient(
			option.WithAPIKey("test-key"),
			option.WithBaseURL(srv.URL),
			option.WithMaxRetries(0),
		),
		model:   DefaultModel,
		enabled: true,
	}
}

func replyWith(t *testing.T, body Summary) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		payload, _ := json.Marshal(body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model":       DefaultModel,
			"content":     []map[string]any{{"type": "text", "text": string(payload)}},
			"usage":       map[string]any{"input_tokens": 812, "output_tokens": 96},
			"stop_reason": "end_turn",
		})
	}
}

func TestDisabledSummarizerRefusesWithoutCallingOut(t *testing.T) {
	s := New("", "")

	if s.Enabled() {
		t.Fatal("summarizer without an API key reported itself as enabled")
	}
	if _, err := s.Summarize(context.Background(), sampleContext()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Summarize error = %v, want ErrDisabled", err)
	}
}

func TestDisabledSummarizerStillReportsAModel(t *testing.T) {
	if got := New("", "").Model(); got != DefaultModel {
		t.Fatalf("Model() = %q, want %q so callers can report the configured model", got, DefaultModel)
	}
}

func TestNewDefaultsToOpus5(t *testing.T) {
	if got := New("key", "").Model(); got != "claude-opus-5" {
		t.Fatalf("default model = %q, want claude-opus-5", got)
	}
	if got := New("key", "claude-haiku-4-5").Model(); got != "claude-haiku-4-5" {
		t.Fatalf("explicit model was overridden, got %q", got)
	}
}

func TestSummarizeParsesStructuredResponse(t *testing.T) {
	want := Summary{
		Summary:         "The payment job timed out waiting on the billing gateway.",
		LikelyCause:     "billing-gateway exceeded the 30s job timeout",
		SuggestedAction: "raise timeout_secs or check billing-gateway health, then retry",
		Category:        "timeout",
		Confidence:      "high",
		IsTransient:     true,
	}

	s := stubServer(t, replyWith(t, want))
	got, err := s.Summarize(context.Background(), sampleContext())
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if got.Summary != want.Summary || got.Category != want.Category ||
		got.Confidence != want.Confidence || got.IsTransient != want.IsTransient {
		t.Fatalf("parsed summary = %+v, want %+v", got, want)
	}
	if got.Model != DefaultModel {
		t.Fatalf("summary model = %q, want %q", got.Model, DefaultModel)
	}
	if got.InputTokens != 812 || got.OutputTokens != 96 {
		t.Fatalf("token usage = %d in / %d out, want 812 / 96", got.InputTokens, got.OutputTokens)
	}
}

func TestSummarizeSendsSchemaConstrainedRequest(t *testing.T) {
	var captured map[string]any

	s := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		replyWith(t, Summary{
			Summary: "s", LikelyCause: "c", SuggestedAction: "a",
			Category: "timeout", Confidence: "low",
		})(w, r)
	})

	if _, err := s.Summarize(context.Background(), sampleContext()); err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if captured["model"] != DefaultModel {
		t.Errorf("request model = %v, want %s", captured["model"], DefaultModel)
	}

	outputConfig, ok := captured["output_config"].(map[string]any)
	if !ok {
		t.Fatal("request carried no output_config, the response would be unconstrained prose")
	}
	if outputConfig["effort"] != "low" {
		t.Errorf("effort = %v, want low for a short triage call", outputConfig["effort"])
	}
	format, ok := outputConfig["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("output_config.format = %v, want a json_schema constraint", outputConfig["format"])
	}
}

func TestSummarizeRejectsMalformedModelOutput(t *testing.T) {
	s := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": DefaultModel,
			"content":     []map[string]any{{"type": "text", "text": "not json at all"}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
			"stop_reason": "end_turn",
		})
	})

	_, err := s.Summarize(context.Background(), sampleContext())
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Summarize error = %v, want ErrUpstream", err)
	}
}

func TestSummarizeRejectsIncompleteModelOutput(t *testing.T) {
	s := stubServer(t, replyWith(t, Summary{Summary: "", Category: "timeout", Confidence: "high"}))

	if _, err := s.Summarize(context.Background(), sampleContext()); !errors.Is(err, ErrUpstream) {
		t.Fatalf("Summarize error = %v, want ErrUpstream for a response missing fields", err)
	}
}

func TestSummarizeSurfacesRefusals(t *testing.T) {
	s := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": DefaultModel,
			"content":      []map[string]any{},
			"usage":        map[string]any{"input_tokens": 1, "output_tokens": 0},
			"stop_reason":  "refusal",
			"stop_details": map[string]any{"type": "refusal", "explanation": "declined"},
		})
	})

	if _, err := s.Summarize(context.Background(), sampleContext()); !errors.Is(err, ErrRefused) {
		t.Fatalf("Summarize error = %v, want ErrRefused", err)
	}
}

func TestSummarizeClassifiesProviderErrors(t *testing.T) {
	tests := []struct {
		status int
		expect string
	}{
		{401, "credentials"},
		{429, "rate limit"},
		{529, "overloaded"},
		{400, "provider returned 400"},
	}

	for _, tc := range tests {
		s := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`))
		})

		_, err := s.Summarize(context.Background(), sampleContext())
		if !errors.Is(err, ErrUpstream) {
			t.Errorf("status %d: error = %v, want ErrUpstream", tc.status, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.expect) {
			t.Errorf("status %d: message %q does not mention %q", tc.status, err, tc.expect)
		}
	}
}

func TestRenderIncludesEveryEvidenceSection(t *testing.T) {
	rendered := sampleContext().Render()

	for _, want := range []string{
		"process_payment", "payments", "attempts: 3 of 3", "timeout_secs: 30",
		"terminal_error", "billing-gateway", "payload", "logs",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered context is missing %q", want)
		}
	}
}

func TestRenderMarksMissingEvidenceExplicitly(t *testing.T) {
	rendered := FailureContext{JobType: "empty", Status: "failed"}.Render()

	if strings.Count(rendered, "none recorded") < 3 {
		t.Fatalf("a job with no error, attempts or logs should say so explicitly:\n%s", rendered)
	}
}

func TestRenderTruncatesRunawayErrors(t *testing.T) {
	fc := sampleContext()
	fc.LastError = strings.Repeat("x", maxErrorChars*3)

	rendered := fc.Render()
	if len(rendered) > maxErrorChars*2 {
		t.Fatalf("rendered context is %d chars, a runaway error was not truncated", len(rendered))
	}
	if !strings.Contains(rendered, "truncated") {
		t.Fatal("truncation happened silently, the model cannot tell evidence was cut")
	}
}

func TestRenderCapsLogVolume(t *testing.T) {
	fc := sampleContext()
	fc.Logs = make([]LogSample, maxLogLines*3)
	for i := range fc.Logs {
		fc.Logs[i] = LogSample{Level: "info", Message: "line"}
	}

	rendered := fc.Render()
	if strings.Count(rendered, "[info] line") > maxLogLines {
		t.Fatalf("more than %d log lines reached the prompt", maxLogLines)
	}
	if !strings.Contains(rendered, "earlier lines omitted") {
		t.Fatal("dropped log lines were not accounted for in the prompt")
	}
}

func TestFingerprintIsStableForIdenticalEvidence(t *testing.T) {
	a, b := sampleContext(), sampleContext()

	if a.Fingerprint(DefaultModel) != b.Fingerprint(DefaultModel) {
		t.Fatal("identical failure evidence produced different fingerprints, caching would never hit")
	}
}

func TestFingerprintChangesWithEvidenceOrModel(t *testing.T) {
	base := sampleContext()
	baseline := base.Fingerprint(DefaultModel)

	changed := base
	changed.LastError = "a completely different failure"
	if changed.Fingerprint(DefaultModel) == baseline {
		t.Error("fingerprint ignored a changed error, stale summaries would be served forever")
	}

	if base.Fingerprint("claude-haiku-4-5") == baseline {
		t.Error("fingerprint ignored the model, switching models would not regenerate summaries")
	}
}

func TestSchemaConstrainsCategoryAndConfidence(t *testing.T) {
	props, ok := schema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties block")
	}

	category, _ := props["category"].(map[string]any)
	if _, ok := category["enum"]; !ok {
		t.Error("category is unconstrained, the model could invent categories")
	}

	confidence, _ := props["confidence"].(map[string]any)
	enum, _ := confidence["enum"].([]string)
	if len(enum) != 3 {
		t.Errorf("confidence enum = %v, want exactly low/medium/high to match the database constraint", enum)
	}

	if schema()["additionalProperties"] != false {
		t.Error("schema allows additional properties, responses could carry unvalidated fields")
	}
}

func TestSchemaCategoriesMatchDocumentedSet(t *testing.T) {
	props := schema()["properties"].(map[string]any)
	category := props["category"].(map[string]any)
	enum, _ := category["enum"].([]string)

	if len(enum) != len(categories) {
		t.Fatalf("schema exposes %d categories, the package defines %d", len(enum), len(categories))
	}
	if !strings.Contains(strings.Join(enum, ","), "unknown") {
		t.Error("category set has no fallback value, the model would be forced to guess")
	}
}
