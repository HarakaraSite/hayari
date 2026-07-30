// Package titletranslation runs manually requested, title-only translations.
package titletranslation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

const (
	maxJobs        = 4
	maxHenji       = 2
	maxInputBytes  = 4 << 10
	maxOutputBytes = 4 << 10
	maxTitleRunes  = 500
)

type Config struct{ Path, API, Model string }

func DefaultConfig() Config { return Config{"henji", "openrouter", "google/gemini-2.5-flash-lite"} }

type Manager struct {
	db          *storage.Storage
	config      Config
	ctx         context.Context
	cancel      context.CancelFunc
	jobs, henji chan struct{}
	wg          sync.WaitGroup
	mu          sync.Mutex
	stopping    bool
}

func New(db *storage.Storage, config Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{db: db, config: config, ctx: ctx, cancel: cancel, jobs: make(chan struct{}, maxJobs), henji: make(chan struct{}, maxHenji)}
}
func (m *Manager) Available() bool {
	if m == nil || m.config.Path == "" {
		return false
	}
	_, err := exec.LookPath(m.config.Path)
	return err == nil
}

// Start never queues work when unavailable, stopping, full, or already active for this feed.
func (m *Manager) Start(feedID int64) (int, error) {
	if !m.Available() {
		return 0, nil
	}
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return 0, nil
	}
	select {
	case m.jobs <- struct{}{}:
	default:
		m.mu.Unlock()
		return 0, nil
	}
	claim, items, err := m.db.ClaimTitleTranslations(feedID, 50)
	if err != nil || len(items) == 0 {
		<-m.jobs
		m.mu.Unlock()
		return 0, err
	}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() { defer m.wg.Done(); defer func() { <-m.jobs }(); m.run(claim, items) }()
	return len(items), nil
}
func (m *Manager) run(claim string, items []storage.Item) {
	defer m.db.ReleaseTitleTranslationClaim(claim)
	for _, item := range items {
		if m.ctx.Err() != nil {
			return
		}
		eligible, err := m.db.TranslationItemStillEligible(item.ID, claim)
		if err != nil {
			continue
		}
		if !eligible {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationPending, nil)
			continue
		}
		if len([]byte(item.Title)) > maxInputBytes || !utf8.ValidString(item.Title) {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationFailed, nil)
			continue
		}
		if !hasLatin(item.Title) {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationSkipped, nil)
			continue
		}
		result, title, err := m.translate(item.Title)
		if err != nil {
			if m.ctx.Err() != nil {
				return
			}
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationFailed, nil)
			continue
		}
		if result == "skipped" {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationSkipped, nil)
		} else {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationTranslated, &title)
		}
	}
}
func hasLatin(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

const schema = `{"type":"object","additionalProperties":false,"required":["result"],"properties":{"result":{"type":"string","enum":["translated","skipped"]},"title":{"type":"string"}},"allOf":[{"if":{"properties":{"result":{"const":"translated"}}},"then":{"required":["title"]}},{"if":{"properties":{"result":{"const":"skipped"}}},"then":{"not":{"required":["title"]}}}]}`
const prompt = "Determine whether the input title is English. If it is English, translate it into natural Japanese. Return translated with title; otherwise return skipped."

type henjiResult struct {
	Result string  `json:"result"`
	Title  *string `json:"title"`
}
type cappedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
func (m *Manager) translate(title string) (string, string, error) {
	if len([]byte(title)) > maxInputBytes || !utf8.ValidString(title) {
		return "", "", errors.New("invalid title input")
	}
	select {
	case m.henji <- struct{}{}:
	case <-m.ctx.Done():
		return "", "", m.ctx.Err()
	}
	defer func() { <-m.henji }()
	ctx, cancel := context.WithTimeout(m.ctx, 30e9)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.config.Path, "-q", "-a", m.config.API, "-m", m.config.Model, "--no-cache", "--max-tokens", "512", "--json-schema", schema, "--json-schema-retries", "0", prompt)
	cmd.Stdin = strings.NewReader(title)
	var out cappedBuffer
	out.max = maxOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || out.overflow {
		return "", "", errors.New("henji failed")
	}
	if !utf8.Valid(out.Bytes()) {
		return "", "", errors.New("invalid utf8 output")
	}
	raw, err := decodeStrictObject(out.Bytes())
	if err != nil {
		return "", "", err
	}
	if len(raw) < 1 || len(raw) > 2 || raw["result"] == nil {
		return "", "", errors.New("invalid result fields")
	}
	for key := range raw {
		if key != "result" && key != "title" {
			return "", "", errors.New("unknown result field")
		}
	}
	var result henjiResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return "", "", err
	}
	if result.Result == "skipped" && result.Title == nil {
		if _, present := raw["title"]; present {
			return "", "", errors.New("skipped result has title")
		}
		return result.Result, "", nil
	}
	if result.Result == "translated" && result.Title != nil && strings.TrimSpace(*result.Title) != "" && utf8.RuneCountInString(*result.Title) <= maxTitleRunes {
		return result.Result, *result.Title, nil
	}
	return "", "", errors.New("invalid result")
}

// decodeStrictObject rejects duplicate members as well as trailing JSON values.
func decodeStrictObject(data []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("result is not an object")
	}
	object := make(map[string]json.RawMessage)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid result key")
		}
		if _, exists := object[key]; exists {
			return nil, errors.New("duplicate result field")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		object[key] = value
	}
	if token, err := dec.Token(); err != nil {
		return nil, err
	} else if delim, ok := token.(json.Delim); !ok || delim != '}' {
		return nil, errors.New("invalid result object")
	}
	if token, err := dec.Token(); err != io.EOF || token != nil {
		return nil, errors.New("extra JSON output")
	}
	return object, nil
}

// Stop closes admission, cancels running Henji processes and releases claims.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.stopping = true
	m.mu.Unlock()
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	timedOut := false
	select {
	case <-done:
	case <-ctx.Done():
		// Do not return while a worker may still use SQLite: Server.Stop is
		// followed by db.Close. Henji has already received cancellation.
		timedOut = true
		<-done
	}
	if err := m.db.ReleaseProcessingTitleTranslations(); err != nil {
		return err
	}
	if timedOut {
		return ctx.Err()
	}
	return nil
}
