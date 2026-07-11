package intent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xolo/backend/internal/config"
)

// DefaultCloudflareModel is the Workers AI text-generation model used when
// config leaves cloudflare.model blank. The 8B instruct model reliably handles
// colloquial phrasings ("slack this", "archive this") that the 1B model misses,
// while staying inexpensive. Swap it via config without touching code.
const DefaultCloudflareModel = "@cf/meta/llama-3.1-8b-instruct"

// classifyTimeout bounds a single Workers AI request so a slow model can never
// hang the caller. On timeout the classifier returns NoAction.
const classifyTimeout = 5 * time.Second

// systemPrompt is the command catalog. It frames the task exactly as
// "commands on one side, user input on the other": the model maps the user's
// comment to one of the three command strings and replies with strict JSON so
// the output is trivial (and safe) to parse.
const systemPrompt = `You classify a single chat/comment message into exactly one NotifBuddy command.

Commands:
- "create-channel": the message asks to create, open, set up, or start a Slack channel for this PR/ticket (e.g. "create slack channel", "slack this", "open a channel").
- "close-channel": the message asks to close, archive, stop, unsync, or remove the Slack channel (e.g. "close the channel", "archive this", "unsync").
- "no-action": anything else — ordinary discussion, questions, or unrelated text. This is the default; prefer it whenever the message is not clearly one of the two commands above.

Reply with ONLY a compact JSON object and nothing else, in the form:
{"action":"create-channel"} or {"action":"close-channel"} or {"action":"no-action"}`

// CloudflareClassifier implements Classifier against the Cloudflare Workers AI
// REST API (POST .../ai/run/{model}). It is safe for concurrent use.
type CloudflareClassifier struct {
	accountID string
	apiToken  string
	model     string
	http      *http.Client
}

// NewCloudflareClassifier builds a classifier from config. Missing credentials
// are not an error: the resulting classifier is "unconfigured" and Classify
// returns NoAction (logged once per call site via the unconfigured path). The
// model falls back to DefaultCloudflareModel when blank.
func NewCloudflareClassifier(cfg config.CloudflareConfig) *CloudflareClassifier {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultCloudflareModel
	}
	return &CloudflareClassifier{
		accountID: strings.TrimSpace(cfg.AccountID),
		apiToken:  strings.TrimSpace(cfg.APIToken),
		model:     model,
		http:      &http.Client{Timeout: classifyTimeout},
	}
}

// configured reports whether the account id and API token are both present.
func (c *CloudflareClassifier) configured() bool {
	return c.accountID != "" && c.apiToken != ""
}

// cfRunRequest is the Workers AI request body for instruct models. The messages
// array carries the system command-catalog and the raw user text.
type cfRunRequest struct {
	Messages []cfMessage `json:"messages"`
}

type cfMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// cfRunResponse is the subset of the Workers AI envelope we read. Workers AI is
// not consistent about result.response across models: simpler models return it
// as a string ("{...}" or a bare command), while chat models in JSON mode (e.g.
// llama-3.1-8b-instruct) return it as a JSON object and also populate an
// OpenAI-style result.choices[].message.content. We keep response as RawMessage
// and try each shape in resultText.
type cfRunResponse struct {
	Result struct {
		Response json.RawMessage `json:"response"`
		Choices  []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"result"`
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// resultText pulls the model's answer out of the envelope as a single string for
// parseAction, tolerating every shape Workers AI returns:
//   - response as a JSON string  -> the unwrapped string
//   - response as a JSON object  -> the raw object text (e.g. {"action":"..."})
//   - choices[0].message.content -> the OpenAI-style content string
func (r cfRunResponse) resultText() string {
	if raw := r.Result.Response; len(raw) > 0 {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s // response was a JSON string
		}
		return string(raw) // response was a JSON object/other; hand the raw text to parseAction
	}
	if len(r.Result.Choices) > 0 {
		return r.Result.Choices[0].Message.Content
	}
	return ""
}

// Classify sends the raw comment text to Workers AI and maps the model's reply
// to an Intent. Every failure mode resolves to NoAction.
func (c *CloudflareClassifier) Classify(ctx context.Context, text string) Intent {
	if !c.configured() {
		slog.WarnContext(ctx, "intent: cloudflare classifier not configured — returning no-action")
		return NoAction
	}
	if strings.TrimSpace(text) == "" {
		return NoAction
	}

	body, err := json.Marshal(cfRunRequest{Messages: []cfMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}})
	if err != nil {
		slog.ErrorContext(ctx, "intent: marshal request failed", "error", err)
		return NoAction
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s",
		c.accountID, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "intent: build request failed", "error", err)
		return NoAction
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "intent: cloudflare request failed", "model", c.model, "error", err)
		return NoAction
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "intent: cloudflare unexpected status", "status", resp.StatusCode, "model", c.model)
		return NoAction
	}

	var out cfRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.ErrorContext(ctx, "intent: decode response failed", "error", err)
		return NoAction
	}
	if !out.Success {
		slog.ErrorContext(ctx, "intent: cloudflare reported failure", "errors", out.Errors, "model", c.model)
		return NoAction
	}

	return parseAction(out.resultText())
}

// parseAction extracts the command from the model's text. The model is asked for
// strict JSON ({"action":"..."}); we also tolerate the bare command string in
// case a small model omits the wrapper. Anything not exactly one of the three
// commands becomes NoAction.
func parseAction(response string) Intent {
	response = strings.TrimSpace(response)

	// Preferred path: a JSON object somewhere in the text. Small models
	// occasionally wrap output in prose or code fences, so slice to the first
	// {...} rather than requiring the whole string to be JSON.
	if start := strings.IndexByte(response, '{'); start >= 0 {
		if end := strings.LastIndexByte(response, '}'); end > start {
			var parsed struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(response[start:end+1]), &parsed); err == nil {
				if action := strings.TrimSpace(parsed.Action); known(action) {
					return Intent(action)
				}
			}
		}
	}

	// Fallback: the bare command string (e.g. the model replied "create-channel").
	if known(response) {
		return Intent(response)
	}
	return NoAction
}
