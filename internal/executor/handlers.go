package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tyagiquamar/durablemcp/internal/store"
)

// DemoTools is the registry of tools this executor knows how to run. The point
// is not what they do -- their execution is governed by the fencing contract.
func DemoTools() []store.Tool {
	obj := func(props string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{%s}}`, props))
	}
	return []store.Tool{
		{
			Name:         "slow_compute",
			Description:  "Sleep N seconds then return a result. Simulates long-running work.",
			InputSchema:  obj(`"seconds":{"type":"integer"},"fail":{"type":"boolean"}`),
			MaxAttempts:  3,
			LeaseSeconds: 30,
		},
		{
			Name:         "call_api",
			Description:  "GET an external URL and return the response body. Read-only, safe to retry.",
			InputSchema:  obj(`"url":{"type":"string"},"fail":{"type":"boolean"}`),
			MaxAttempts:  3,
			LeaseSeconds: 30,
		},
		{
			Name:         "write_file",
			Description:  "Write content to a path under the executor scratch dir. Idempotent by path+hash.",
			InputSchema:  obj(`"path":{"type":"string"},"content":{"type":"string"},"fail":{"type":"boolean"}`),
			MaxAttempts:  3,
			LeaseSeconds: 30,
		},
		{
			Name:         "send_webhook",
			Description:  "POST JSON to a URL. Side-effecting -- supply an idempotency key for external writes.",
			InputSchema:  obj(`"url":{"type":"string"},"body":{"type":"object"},"fail":{"type":"boolean"}`),
			MaxAttempts:  3,
			LeaseSeconds: 30,
		},
	}
}

// Handlers returns the tool handler map for the runtime. scratchDir is the base
// directory write_file is confined to.
func Handlers(scratchDir string) map[string]Handler {
	return map[string]Handler{
		"slow_compute": slowCompute,
		"call_api":     callAPI,
		"write_file":   writeFile(scratchDir),
		"send_webhook": sendWebhook,
	}
}

// forcedFailure lets the retry-exhaustion demo drive any tool into failure by
// passing {"fail": true}.
func forcedFailure(args json.RawMessage) error {
	var probe struct {
		Fail bool `json:"fail"`
	}
	_ = json.Unmarshal(args, &probe)
	if probe.Fail {
		return errors.New("forced failure (fail=true)")
	}
	return nil
}

func slowCompute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := forcedFailure(args); err != nil {
		return nil, err
	}
	var in struct {
		Seconds int `json:"seconds"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.Seconds <= 0 {
		in.Seconds = 1
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(in.Seconds) * time.Second):
	}
	return marshal(map[string]any{"slept_seconds": in.Seconds, "computed_at": time.Now().UTC()})
}

func callAPI(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := forcedFailure(args); err != nil {
		return nil, err
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.URL == "" {
		return nil, errors.New("url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return marshal(map[string]any{"status": resp.StatusCode, "body": string(body)})
}

func writeFile(scratchDir string) Handler {
	return func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		if err := forcedFailure(args); err != nil {
			return nil, err
		}
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if in.Path == "" {
			return nil, errors.New("path is required")
		}
		// Confine writes to scratchDir; reject path traversal.
		clean := filepath.Clean("/" + strings.ReplaceAll(in.Path, "\\", "/"))
		target := filepath.Join(scratchDir, clean)
		if !strings.HasPrefix(target, filepath.Clean(scratchDir)) {
			return nil, errors.New("path escapes scratch directory")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(in.Content), 0o644); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(in.Content))
		return marshal(map[string]any{"path": target, "bytes_written": len(in.Content), "sha256": hex.EncodeToString(sum[:])})
	}
}

func sendWebhook(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if err := forcedFailure(args); err != nil {
		return nil, err
	}
	var in struct {
		URL  string          `json:"url"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.URL == "" {
		return nil, errors.New("url is required")
	}
	if len(in.Body) == 0 {
		in.Body = json.RawMessage(`{}`)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.URL, bytes.NewReader(in.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return marshal(map[string]any{"status": resp.StatusCode, "delivered_at": time.Now().UTC()})
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
