package agent

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"sort"
	"time"

	"hermetrix-harness/internal/embedding"
)

// Semantic retrieval closes O-44.
//
// Measured on the phrasing corpus: when a fact was stated in words the question
// did not use and sat where compaction discards content, the model searched for
// it 18 times out of 19 and context_search found it 3 times. The model queried
// in the question's words; the fact was written in different ones; and that is
// the same mismatch that made the compactor drop it. Adding retries or better
// prompting would not have helped -- the retriever and the ranker were the same
// lexical function failing on the same input.
//
// What this does not do is replace lexical matching. In the same run, where the
// wording did match, lexical retrieval was perfect: far/head searched 7 and
// found 7, near/head 2 for 2. An exact identifier is something a substring
// finds and a vector approximates. The two are unioned, and a result found by
// both ranks above one found by either.

// SetEmbedder attaches an embedder. Nil disables semantic retrieval, which is a
// supported configuration rather than a broken one: Hermetrix is local-first
// and an embedder is a second model to run.
func (s *Service) SetEmbedder(embedder embedding.Embedder) { s.embedder = embedder }

// embedNewEvents stores vectors for events that do not have one at the current
// revision. It is best-effort by design -- a turn must not fail because an
// optional index could not be updated -- and returns the number written so a
// caller can tell "nothing to do" from "the embedder is down".
func (s *Service) embedNewEvents(ctx context.Context, sessionID string) (int, error) {
	if s.embedder == nil {
		return 0, embedding.ErrNoEmbedder
	}
	revision := s.embedder.Revision()
	rows, err := s.store.DB.QueryContext(ctx, `SELECT e.id, e.content FROM agent_events e
      LEFT JOIN event_embeddings v ON v.event_id = e.id AND v.revision = ?
      WHERE e.session_id = ? AND v.event_id IS NULL AND e.content <> ''
        AND e.event_kind IN ('message','tool_call','tool_result','approval_decision','turn_failed')
      ORDER BY e.sequence`, revision, sessionID)
	if err != nil {
		return 0, err
	}
	var ids, texts []string
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		texts = append(texts, boundedText(content, embeddedTextLimit))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	now := formatTime(time.Now().UTC())
	written := 0
	for index, vector := range vectors {
		if _, err := s.store.DB.ExecContext(ctx, `INSERT OR REPLACE INTO event_embeddings
          (event_id,session_id,revision,dimensions,vector,created_at) VALUES(?,?,?,?,?,?)`,
			ids[index], sessionID, revision, len(vector), encodeVector(vector), now); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// embeddedTextLimit bounds what is embedded per event. A model's own context is
// the real limit and it is smaller than a large tool result; truncating here
// makes that explicit rather than letting the provider decide silently.
const embeddedTextLimit = 4000

// semanticMatches ranks a session's events against a query by cosine
// similarity and returns the ones that stand out from the session's own
// baseline.
//
// Selection is relative, not a fixed threshold, because a fixed one does not
// transfer. Measured with bge-m3 on real Thai:
//
//	question about batch size  vs its paraphrase   0.480
//	                           vs unrelated prose  0.419
//	question about a plan id   vs its paraphrase   0.604
//	                           vs unrelated prose  0.471
//
// The ranking is right in both cases and the absolute values overlap: any cut
// that keeps the first paraphrase admits the second question's noise. A
// bi-encoder puts everything in one language on a narrow, query-dependent band,
// so what matters is standing above that band rather than above a constant.
func (s *Service) semanticMatches(ctx context.Context, sessionID, query string) (map[string]float64, error) {
	if s.embedder == nil {
		return nil, embedding.ErrNoEmbedder
	}
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, err
	}
	queryVector := vectors[0]
	revision := s.embedder.Revision()
	rows, err := s.store.DB.QueryContext(ctx, `SELECT event_id, vector FROM event_embeddings
      WHERE session_id = ? AND revision = ?`, sessionID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scoredEvent struct {
		id    string
		score float64
	}
	var scored []scoredEvent
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		scored = append(scored, scoredEvent{id: id, score: embedding.Cosine(queryVector, decodeVector(blob))})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(scored) == 0 {
		return map[string]float64{}, nil
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	baseline := scored[len(scored)/2].score
	matches := map[string]float64{}
	for _, item := range scored {
		if len(matches) >= SemanticTopK {
			break
		}
		if item.score < baseline+SemanticMargin {
			break
		}
		matches[item.id] = item.score
	}
	return matches, nil
}

// SemanticTopK bounds how many semantic hits are folded into a search. The
// result list is small and a vector search that returns everything it ranked
// would drown the lexical hits it is meant to complement.
const SemanticTopK = 5

// SemanticMargin is how far above the session's median similarity a match must
// sit. It is small because the band is narrow -- with bge-m3 the gap between a
// paraphrase and unrelated prose was 0.06 on one query and 0.13 on another --
// and because a weak semantic hit costs a line in a list while a missed one
// costs the answer.
const SemanticMargin = 0.05

// encodeVector stores a vector as little-endian float32. SQLite has no vector
// type and a BLOB of known width is both compact and exactly reversible, which
// a JSON array of decimals is not.
func encodeVector(vector []float32) []byte {
	out := make([]byte, 4*len(vector))
	for index, value := range vector {
		binary.LittleEndian.PutUint32(out[index*4:], math.Float32bits(value))
	}
	return out
}

func decodeVector(blob []byte) []float32 {
	vector := make([]float32, len(blob)/4)
	for index := range vector {
		vector[index] = math.Float32frombits(binary.LittleEndian.Uint32(blob[index*4:]))
	}
	return vector
}
