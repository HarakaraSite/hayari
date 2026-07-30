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
		if !hasLatin(item.Title) {
			_ = m.db.SetTitleTranslationResult(item.ID, claim, storage.TitleTranslationSkipped, nil)
			continue
		}
		result, title, err := m.translate(item.Title)
		if err != nil {
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
	var result henjiResult
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return "", "", err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return "", "", errors.New("extra JSON output")
	}
	if result.Result == "skipped" && result.Title == nil {
		return result.Result, "", nil
	}
	if result.Result == "translated" && result.Title != nil && strings.TrimSpace(*result.Title) != "" && utf8.RuneCountInString(*result.Title) <= maxTitleRunes {
		return result.Result, *result.Title, nil
	}
	return "", "", errors.New("invalid result")
}
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stopping = true
	m.mu.Unlock()
	m.cancel()
	m.wg.Wait()
	_ = m.db.ReleaseProcessingTitleTranslations()
}
