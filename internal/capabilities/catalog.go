package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNotFound         = errors.New("capability not found")
	ErrRevisionConflict = errors.New("capability revision conflict")
	ErrNotReady         = errors.New("capability is not ready")
	ErrExecutorMissing  = errors.New("capability executor is unavailable")
)

const (
	SourceMCP       = "mcp"
	ReadinessReady  = "ready"
	ReadinessLocked = "credential_missing"
	ReadinessOff    = "disabled"
	ReadinessStale  = "stale"
)

// Entry is the canonical, revision-bound description of one deferred
// capability. Its schema is intentionally omitted from SearchResult so catalog
// size cannot grow the model's direct tool prompt.
type Entry struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Title            string          `json:"title,omitempty"`
	Description      string          `json:"description"`
	Source           string          `json:"source"`
	SourceRef        string          `json:"source_ref"`
	Revision         string          `json:"revision"`
	Effect           string          `json:"effect"`
	Readiness        string          `json:"readiness"`
	RequiresApproval bool            `json:"requires_approval"`
	InputSchema      json.RawMessage `json:"input_schema"`
	OutputSchema     json.RawMessage `json:"output_schema,omitempty"`
	Annotations      json.RawMessage `json:"annotations,omitempty"`
	Metadata         map[string]any  `json:"metadata,omitempty"`
}

type SearchResult struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Title            string `json:"title,omitempty"`
	Description      string `json:"description"`
	Source           string `json:"source"`
	SourceRef        string `json:"source_ref"`
	Effect           string `json:"effect"`
	Readiness        string `json:"readiness"`
	RequiresApproval bool   `json:"requires_approval"`
}

type CallResult struct {
	Output   string         `json:"output"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Executor interface {
	ExecuteCapability(context.Context, Entry, json.RawMessage) (CallResult, error)
}

type Summary struct {
	Total       int            `json:"total"`
	BySource    map[string]int `json:"by_source"`
	ByReadiness map[string]int `json:"by_readiness"`
}

type Catalog struct {
	mu        sync.RWMutex
	entries   map[string]Entry
	executors map[string]Executor
}

func NewCatalog() *Catalog {
	return &Catalog{entries: map[string]Entry{}, executors: map[string]Executor{}}
}

func (c *Catalog) SetExecutor(source string, executor Executor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if executor == nil {
		delete(c.executors, source)
		return
	}
	c.executors[source] = executor
}

// ReplaceSourceRef atomically replaces one provider/server snapshot. This
// prevents callers from observing half of a paginated discovery result.
func (c *Catalog) ReplaceSourceRef(source, sourceRef string, entries []Entry) error {
	prepared := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Name == "" || entry.Source != source || entry.SourceRef != sourceRef || entry.Revision == "" {
			return fmt.Errorf("invalid catalog entry for %s/%s", source, sourceRef)
		}
		if !json.Valid(entry.InputSchema) {
			return fmt.Errorf("capability %s has invalid input schema", entry.ID)
		}
		prepared[entry.ID] = cloneEntry(entry)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, entry := range c.entries {
		if entry.Source == source && entry.SourceRef == sourceRef {
			delete(c.entries, id)
		}
	}
	for id, entry := range prepared {
		c.entries[id] = entry
	}
	return nil
}

func (c *Catalog) Search(query, source string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if runes := []rune(query); len(runes) > 512 {
		query = string(runes[:512])
	}
	source = strings.TrimSpace(source)
	type scored struct {
		entry Entry
		score int
	}
	c.mu.RLock()
	items := make([]scored, 0, len(c.entries))
	for _, entry := range c.entries {
		if source != "" && entry.Source != source {
			continue
		}
		name := strings.ToLower(entry.Name)
		title := strings.ToLower(entry.Title)
		description := strings.ToLower(entry.Description)
		if query != "" && !strings.Contains(name, query) && !strings.Contains(title, query) && !strings.Contains(description, query) {
			continue
		}
		score := 0
		if query != "" {
			if name == query {
				score += 100
			}
			if strings.HasPrefix(name, query) {
				score += 50
			}
			if strings.Contains(name, query) {
				score += 20
			}
			if strings.Contains(title, query) {
				score += 10
			}
			if strings.Contains(description, query) {
				score += 5
			}
		}
		if entry.Readiness == ReadinessReady {
			score++
		}
		items = append(items, scored{entry: entry, score: score})
	}
	c.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if items[i].entry.SourceRef != items[j].entry.SourceRef {
			return items[i].entry.SourceRef < items[j].entry.SourceRef
		}
		if items[i].entry.Name != items[j].entry.Name {
			return items[i].entry.Name < items[j].entry.Name
		}
		return items[i].entry.ID < items[j].entry.ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		entry := item.entry
		results = append(results, SearchResult{ID: entry.ID, Name: entry.Name, Title: entry.Title,
			Description: entry.Description, Source: entry.Source, SourceRef: entry.SourceRef, Effect: entry.Effect,
			Readiness: entry.Readiness, RequiresApproval: entry.RequiresApproval})
	}
	return results
}

func (c *Catalog) Describe(id string) (Entry, error) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	c.mu.RUnlock()
	if !ok {
		return Entry{}, ErrNotFound
	}
	return cloneEntry(entry), nil
}

func (c *Catalog) Call(ctx context.Context, id, revision string, arguments json.RawMessage) (CallResult, Entry, error) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	executor := c.executors[entry.Source]
	c.mu.RUnlock()
	if !ok {
		return CallResult{}, Entry{}, ErrNotFound
	}
	entry = cloneEntry(entry)
	if revision == "" || revision != entry.Revision {
		return CallResult{}, entry, fmt.Errorf("%w: expected %s, current %s", ErrRevisionConflict, revision, entry.Revision)
	}
	if entry.Readiness != ReadinessReady {
		return CallResult{}, entry, fmt.Errorf("%w: %s", ErrNotReady, entry.Readiness)
	}
	if executor == nil {
		return CallResult{}, entry, ErrExecutorMissing
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(arguments, &object); err != nil {
		return CallResult{}, entry, fmt.Errorf("capability arguments must be a JSON object: %w", err)
	}
	result, err := executor.ExecuteCapability(ctx, entry, arguments)
	return result, entry, err
}

func (c *Catalog) Summary() Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := Summary{Total: len(c.entries), BySource: map[string]int{}, ByReadiness: map[string]int{}}
	for _, entry := range c.entries {
		result.BySource[entry.Source]++
		result.ByReadiness[entry.Readiness]++
	}
	return result
}

func cloneEntry(entry Entry) Entry {
	entry.InputSchema = append(json.RawMessage(nil), entry.InputSchema...)
	entry.OutputSchema = append(json.RawMessage(nil), entry.OutputSchema...)
	entry.Annotations = append(json.RawMessage(nil), entry.Annotations...)
	if entry.Metadata != nil {
		entry.Metadata = make(map[string]any, len(entry.Metadata))
		for key, value := range entry.Metadata {
			entry.Metadata[key] = value
		}
	}
	return entry
}
