package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/embedding"
)

// Semantic Skill retrieval closes R-14.
//
// The lexical scorer shares no trigram between scripts, so a catalog written in
// English is invisible to a goal written in Thai. Measured against the driven
// corpus's own catalog:
//
//	ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์  -> []
//	round satang half up              -> [money-rounding-thai satang-rounding]
//
// Both lines describe the same procedure. The second retrieves it and the first
// retrieves nothing, and because selectSkillBindings also produces the
// denominator of the retrieval metric, that turn was counted as a turn with no
// relevant Skill -- a blind spot that reported itself as an absence.
//
// What is added is the mechanism already proved on conversation history: embed,
// chunk, and rank relative to a baseline. What is deliberately not added is a
// replacement for the lexical path. An exact canonical name is a substring
// match and a vector only approximates it, so the two scores are summed.

// skillSemanticWeight is the most a semantic match can add, matched to
// gramWeight: at full strength meaning-similarity is worth what the strongest
// lexical signal short of pinning is worth, and no more. A vector that outvoted
// an exact name hit would make the catalog unaddressable by name.
const skillSemanticWeight = 40

// semanticControls are sentences chosen to have nothing to do with any Skill,
// and they are how "this goal matches nothing" becomes expressible.
//
// The catalog cannot answer that on its own. Ranking within it always has a
// winner, and its own spread is not a reliable floor: measured on a real
// catalog with bge-m3, a goal about the weather scored 0.405 against a release
// checklist while the catalog's lower quartile sat at 0.342, so the weather
// cleared the margin and loaded two Skills the turn had no use for. The
// controls give the same query a reading of what unrelated text scores for it,
// which is the number the catalog was being asked to supply and could not.
//
// Deliberately diverse and deliberately mundane, in both scripts, and read as a
// median so that a goal which happens to be about trains or cats is bounded by
// the other four rather than silenced by one. They are embedded once per model
// revision and cached like any other text.
var semanticControls = []string{
	"The cat sleeps on the roof in the afternoon sun",
	"ตารางเดินรถไฟสายเหนือประจำเดือนหน้า",
	"A recipe for slow braised beef with root vegetables",
	"ประวัติศาสตร์การก่อสร้างสะพานข้ามแม่น้ำ",
	"Guitar chord progressions in the key of D minor",
}

// skillEmbeddingTimeout bounds the wait. This runs while the session lease is
// held, and a turn that stalls behind an optional index has turned an
// enhancement into a dependency. Expiring falls back to lexical.
const skillEmbeddingTimeout = 10 * time.Second

// skillEmbeddingText is what represents a Skill to the embedder: its name with
// the hyphens opened out, then its summary. The name is included because it is
// often the most specific thing about a Skill, and "satang-rounding" is not a
// word to a model that has never seen the hyphen form.
func skillEmbeddingText(binding SessionSkillBinding) string {
	name := strings.ReplaceAll(binding.CanonicalName, "-", " ")
	return strings.TrimSpace(name + ". " + binding.Summary)
}

// bindingKey identifies one catalog entry across the scorer and the relevance
// map. Canonical name alone is not enough: two versions of a Skill carry the
// same name and different summaries, which are different vectors.
func bindingKey(binding SessionSkillBinding) string {
	return binding.SkillID + "/" + binding.VersionID
}

func textHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// skillSemanticBonus turns cosine similarities into score the lexical ranker
// can add, and is where the decision about what counts as a match lives.
//
// It is separate from the code that talks to the embedder so the rule can be
// tested without a model, and because the rule is the part that was wrong the
// first three times: a fixed cosine floor does not transfer between queries. A
// bi-encoder puts everything in one language on a narrow, query-dependent band,
// so a match is what stands above this catalog's own noise floor rather than
// above a constant.
//
// The floor is the lower quartile, not the median, and the difference is not
// cosmetic. Conversation history is mostly irrelevant to any one query, so its
// median is a fair estimate of what an unrelated item scores. A Skill catalog
// is not: the corpus catalog R-14 was measured on has three entries and two of
// them are about rounding money. Their scores sit either side of the median,
// which puts the noise floor between two right answers and returns neither. The
// quartile is a more conservative estimate of the same thing, and the asymmetry
// says to be conservative -- a weak hit costs a line in a list, a missed one
// costs the procedure.
//
// The margin does the precision work either way. When nothing in the catalog is
// related to the goal every entry lands on the same narrow band, the best one
// does not clear the floor by SemanticMargin, and the result is empty -- which
// is the answer that keeps an unrelated Skill out of the contract.
func skillSemanticBonus(cosines map[string]float64, controls []float64) map[string]int {
	if len(cosines) == 0 {
		return nil
	}
	scores := make([]float64, 0, len(cosines))
	for _, score := range cosines {
		scores = append(scores, score)
	}
	sort.Float64s(scores)
	baseline := scores[len(scores)/4]
	// Whichever floor is higher. They fail in opposite directions: the catalog's
	// own quartile is too low when nothing in it fits the goal, and the controls
	// are too low when everything in it does, because a catalog of technical
	// summaries all outscores a sentence about a cat. Taking the higher of the
	// two means a Skill has to beat both the unrelated-text reading and its own
	// neighbours.
	if len(controls) > 0 {
		sorted := append([]float64(nil), controls...)
		sort.Float64s(sorted)
		if control := sorted[len(sorted)/2]; control > baseline {
			baseline = control
		}
	}
	// Headroom is what is left between the baseline and a perfect match, so the
	// same absolute gap counts for more on a query whose whole band sits high.
	headroom := 1 - baseline
	if headroom <= 0 {
		return nil
	}
	bonus := map[string]int{}
	for key, score := range cosines {
		if score < baseline+SemanticMargin {
			continue
		}
		weighted := skillSemanticWeight * (score - baseline) / headroom
		if weighted < 1 {
			weighted = 1
		}
		bonus[key] = int(weighted + 0.5)
	}
	return bonus
}

// skillRelevance embeds the goal, embeds any catalog entry not already stored
// at this embedder's revision, and returns the bonus each entry has earned.
//
// Nil on every failure and on every unconfigured case, because the caller's
// fallback is the scorer that shipped before this existed. Nothing here may
// turn "the embedding endpoint is down" into "the turn failed".
func (s *Service) skillRelevance(ctx context.Context, goal string,
	catalog []SessionSkillBinding) map[string]int {
	if s.embedder == nil || strings.TrimSpace(goal) == "" || len(catalog) == 0 {
		return nil
	}
	revision := s.embedder.Revision()
	// One row per chunk, keyed by the hash of the text rather than by the Skill:
	// an edited summary is a different text and gets a different vector without
	// anything having to remember to invalidate the old one, and two Skills that
	// describe themselves identically are embedded once.
	chunksFor := map[string][]string{}
	hashes := map[string]string{}
	for _, binding := range catalog {
		text := skillEmbeddingText(binding)
		hash := textHash(text)
		hashes[bindingKey(binding)] = hash
		if _, seen := chunksFor[hash]; !seen {
			chunksFor[hash] = embedding.Chunk(text)
		}
	}
	controlHashes := make([]string, 0, len(semanticControls))
	for _, control := range semanticControls {
		hash := textHash(control)
		controlHashes = append(controlHashes, hash)
		if _, seen := chunksFor[hash]; !seen {
			chunksFor[hash] = []string{control}
		}
	}
	stored := s.storedSkillVectors(ctx, revision, chunksFor)
	var pendingTexts []string
	type pending struct {
		hash  string
		chunk int
	}
	var pendingKeys []pending
	for hash, chunks := range chunksFor {
		for index, chunk := range chunks {
			if _, have := stored[hash][index]; have {
				continue
			}
			pendingTexts = append(pendingTexts, chunk)
			pendingKeys = append(pendingKeys, pending{hash: hash, chunk: index})
		}
	}
	ctx, cancel := context.WithTimeout(ctx, skillEmbeddingTimeout)
	defer cancel()
	// The goal rides in the same batch as anything missing, so a cold catalog
	// costs one round trip rather than two.
	batch := append([]string{goal}, pendingTexts...)
	vectors, err := s.embedder.Embed(ctx, batch)
	if err != nil || len(vectors) != len(batch) {
		return nil
	}
	goalVector := vectors[0]
	now := formatTime(time.Now().UTC())
	for index, key := range pendingKeys {
		vector := vectors[index+1]
		if stored[key.hash] == nil {
			stored[key.hash] = map[int][]float32{}
		}
		stored[key.hash][key.chunk] = vector
		if _, err := s.store.DB.ExecContext(ctx, `INSERT OR REPLACE INTO skill_embeddings
          (text_hash,revision,chunk,dimensions,vector,created_at) VALUES(?,?,?,?,?,?)`,
			key.hash, revision, key.chunk, len(vector), encodeVector(vector), now); err != nil {
			// A cache that could not be written is a slower next call, not a
			// worse answer: the vectors for this call are already in hand.
			break
		}
	}
	cosines := map[string]float64{}
	for _, binding := range catalog {
		chunks := stored[hashes[bindingKey(binding)]]
		best := 0.0
		for _, vector := range chunks {
			// A Skill is as relevant as its most relevant chunk. Averaging a long
			// summary is what buried the fact in the conversation measurements.
			if score := embedding.Cosine(goalVector, vector); score > best {
				best = score
			}
		}
		cosines[bindingKey(binding)] = best
	}
	controls := make([]float64, 0, len(controlHashes))
	for _, hash := range controlHashes {
		for _, vector := range stored[hash] {
			controls = append(controls, embedding.Cosine(goalVector, vector))
		}
	}
	return skillSemanticBonus(cosines, controls)
}

// storedSkillVectors reads whatever is already cached for these texts. A read
// failure returns an empty map rather than an error: everything it could not
// find is simply embedded again.
func (s *Service) storedSkillVectors(ctx context.Context, revision string,
	chunksFor map[string][]string) map[string]map[int][]float32 {
	stored := map[string]map[int][]float32{}
	if len(chunksFor) == 0 {
		return stored
	}
	arguments := []any{revision}
	placeholders := make([]string, 0, len(chunksFor))
	for hash := range chunksFor {
		placeholders = append(placeholders, "?")
		arguments = append(arguments, hash)
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT text_hash, chunk, vector FROM skill_embeddings
      WHERE revision = ? AND text_hash IN (`+strings.Join(placeholders, ",")+`)`, arguments...)
	if err != nil {
		return stored
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		var chunk int
		var blob []byte
		if err := rows.Scan(&hash, &chunk, &blob); err != nil {
			return stored
		}
		if stored[hash] == nil {
			stored[hash] = map[int][]float32{}
		}
		stored[hash][chunk] = decodeVector(blob)
	}
	return stored
}
