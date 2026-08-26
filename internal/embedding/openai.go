package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAIEmbedder talks to any endpoint that implements POST /v1/embeddings.
//
// That covers ollama, LM Studio, vLLM and llama.cpp as well as hosted
// providers, which is the same reach the chat adapter has and for the same
// reason: the operator should be able to point this at whatever they already
// run rather than at something this project picked for them.
//
// The gateway in use for chat serves no embedding model -- POST /embeddings
// against it returns 404 from the upstream vLLM -- so in practice this points
// at a local runtime while chat goes elsewhere. They are configured separately
// on purpose.
type OpenAIEmbedder struct {
	client     *http.Client
	baseURL    string
	model      string
	apiKey     string
	dimensions int
}

// MaxEmbedBatch bounds one request. Embedding a whole session in a single call
// is how a 413 happens on a runtime nobody tuned; the batch is small enough
// that a local model with a modest context can serve it.
const MaxEmbedBatch = 32

// NewOpenAIEmbedder builds an embedder. dimensions is what the model returns
// and is checked against every response, so a silently swapped model is caught
// on the first call rather than by a search that quietly returns nothing.
func NewOpenAIEmbedder(client *http.Client, baseURL, model, apiKey string, dimensions int) *OpenAIEmbedder {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAIEmbedder{client: client, baseURL: strings.TrimRight(baseURL, "/"),
		model: model, apiKey: apiKey, dimensions: dimensions}
}

func (e *OpenAIEmbedder) Revision() string { return "embed:" + e.model }
func (e *OpenAIEmbedder) Dimensions() int  { return e.dimensions }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += MaxEmbedBatch {
		end := start + MaxEmbedBatch
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings",
		bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body := make([]byte, 400)
		read, _ := response.Body.Read(body)
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d: %s",
			response.StatusCode, strings.TrimSpace(string(body[:read])))
	}
	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding endpoint returned %d vectors for %d inputs",
			len(decoded.Data), len(texts))
	}
	// Order by the index the provider reports rather than trusting arrival
	// order: the field exists because providers are allowed to reorder, and a
	// vector attached to the wrong text is a silent, permanent mislabelling.
	vectors := make([][]float32, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("embedding endpoint returned index %d for %d inputs",
				item.Index, len(texts))
		}
		if e.dimensions > 0 && len(item.Embedding) != e.dimensions {
			return nil, fmt.Errorf("embedding endpoint returned %d dimensions, expected %d",
				len(item.Embedding), e.dimensions)
		}
		vectors[item.Index] = Normalise(item.Embedding)
	}
	for index, vector := range vectors {
		if vector == nil {
			return nil, fmt.Errorf("embedding endpoint returned no vector for input %d", index)
		}
	}
	return vectors, nil
}
