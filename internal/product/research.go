package product

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/identity"
)

var (
	ErrInvalidResearchQuery   = errors.New("research query must contain 2–240 characters")
	ErrInvalidResearchRecency = errors.New("research recency must be 1–365 days")
	ErrResearchNotFound       = errors.New("research article is unavailable to this task")
	ErrResearchEgressBlocked  = errors.New("this task does not allow sending search terms to Crossref")
)

const crossrefWorksURL = "https://api.crossref.org/works"

// ResearchCandidate is bibliographic metadata. Selection does not validate a
// claim or grant permission to republish a publisher's article or abstract.
type ResearchCandidate struct {
	ID                 string    `json:"id"`
	TaskID             string    `json:"task_id"`
	Query              string    `json:"query"`
	SourceKind         string    `json:"source_kind"`
	DOI                string    `json:"doi"`
	Title              string    `json:"title"`
	Publisher          string    `json:"publisher"`
	SourceURL          string    `json:"source_url"`
	PublishedDate      string    `json:"published_date,omitempty"`
	PublishedPrecision string    `json:"published_precision,omitempty"`
	RetrievedAt        time.Time `json:"retrieved_at"`
	SourceDigest       string    `json:"source_digest"`
	Selected           bool      `json:"selected"`
}

type crossrefResponse struct {
	Message struct {
		Items []json.RawMessage `json:"items"`
	} `json:"message"`
}

type crossrefWork struct {
	DOI             string       `json:"DOI"`
	Title           []string     `json:"title"`
	Publisher       string       `json:"publisher"`
	URL             string       `json:"URL"`
	Type            string       `json:"type"`
	Published       crossrefDate `json:"published"`
	PublishedOnline crossrefDate `json:"published-online"`
	PublishedPrint  crossrefDate `json:"published-print"`
	Issued          crossrefDate `json:"issued"`
}

type crossrefDate struct {
	DateParts [][]int `json:"date-parts"`
}

// SearchResearchArticles reads a fixed public metadata service and saves a
// bounded set of results under the trusted owner of this Task.
func (s *Service) SearchResearchArticles(ctx context.Context, taskID, query string, days int) ([]ResearchCandidate, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var egressPolicy string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT egress_policy FROM durable_tasks WHERE id=? AND owner_principal_id=?`, taskID, ownerID).Scan(&egressPolicy); err != nil {
		return nil, err
	}
	if egressPolicy != "remote_allowed" {
		return nil, ErrResearchEgressBlocked
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 240 {
		return nil, ErrInvalidResearchQuery
	}
	if days < 1 || days > 365 {
		return nil, ErrInvalidResearchRecency
	}
	endpoint := s.researchEndpoint
	if endpoint == "" {
		endpoint = crossrefWorksURL
	}
	client := s.researchClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	items, err := fetchCrossref(ctx, client, endpoint, query, days, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i := range items {
		item := &items[i]
		item.ID = identity.New("research")
		item.TaskID = taskID
		item.Query = query
		item.RetrievedAt = now
		_, err = tx.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,0)
			ON CONFLICT(task_id,doi) DO UPDATE SET query=excluded.query,title=excluded.title,publisher=excluded.publisher,
			source_url=excluded.source_url,published_date=excluded.published_date,published_precision=excluded.published_precision,
			retrieved_at=excluded.retrieved_at,source_digest=excluded.source_digest,
			selected=CASE WHEN task_research_candidates.source_digest=excluded.source_digest THEN task_research_candidates.selected ELSE 0 END`,
			item.ID, taskID, ownerID, query, item.SourceKind, item.DOI, item.Title, item.Publisher, item.SourceURL,
			item.PublishedDate, item.PublishedPrecision, formatTime(now), item.SourceDigest)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListResearchArticles(ctx, taskID)
}

func (s *Service) ListResearchArticles(ctx context.Context, taskID string) ([]ResearchCandidate, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected
		FROM task_research_candidates WHERE task_id=? AND owner_principal_id=? ORDER BY selected DESC,published_date DESC,retrieved_at DESC,title LIMIT 100`, taskID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ResearchCandidate{}
	for rows.Next() {
		var item ResearchCandidate
		var retrieved string
		if err := rows.Scan(&item.ID, &item.Query, &item.SourceKind, &item.DOI, &item.Title, &item.Publisher, &item.SourceURL,
			&item.PublishedDate, &item.PublishedPrecision, &retrieved, &item.SourceDigest, &item.Selected); err != nil {
			return nil, err
		}
		item.TaskID = taskID
		item.RetrievedAt, _ = parseTime(retrieved)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SelectResearchArticle(ctx context.Context, taskID, candidateID string, selected bool) (ResearchCandidate, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ResearchCandidate{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_research_candidates SET selected=? WHERE id=? AND task_id=? AND owner_principal_id=?`,
		selected, candidateID, taskID, ownerID)
	if err != nil {
		return ResearchCandidate{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ResearchCandidate{}, ErrResearchNotFound
	}
	items, err := s.ListResearchArticles(ctx, taskID)
	if err != nil {
		return ResearchCandidate{}, err
	}
	for _, item := range items {
		if item.ID == candidateID {
			return item, nil
		}
	}
	return ResearchCandidate{}, ErrResearchNotFound
}

func (s *Service) researchTaskOwner(ctx context.Context, taskID string) (string, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return "", err
	}
	var exists int
	err = s.store.DB.QueryRowContext(ctx, `SELECT 1 FROM durable_tasks WHERE id=? AND owner_principal_id=?`, taskID, ownerID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrResearchNotFound
	}
	return ownerID, err
}

func fetchCrossref(ctx context.Context, client *http.Client, endpoint, query string, days int, now time.Time) ([]ResearchCandidate, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("research endpoint is invalid")
	}
	params := u.Query()
	params.Set("query.bibliographic", query)
	params.Set("filter", "from-pub-date:"+now.AddDate(0, 0, -days).Format("2006-01-02")+",type:journal-article")
	params.Set("rows", "10")
	params.Set("select", "DOI,title,publisher,URL,type,published,published-online,published-print,issued")
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Hermetrix/1.0 (research metadata discovery)")
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("research source unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("research source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("research response exceeds limit")
	}
	var result crossrefResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("research response is invalid: %w", err)
	}
	items := make([]ResearchCandidate, 0, len(result.Message.Items))
	for _, raw := range result.Message.Items {
		if len(items) >= 10 {
			break
		}
		var work crossrefWork
		if json.Unmarshal(raw, &work) != nil {
			continue
		}
		if work.Type != "journal-article" {
			continue
		}
		doi := strings.ToLower(strings.TrimSpace(work.DOI))
		if doi == "" || !strings.HasPrefix(doi, "10.") || strings.ContainsAny(doi, " \t\r\n") || len(doi) > 255 || len(work.Title) == 0 {
			continue
		}
		title := strings.TrimSpace(work.Title[0])
		if title == "" || utf8.RuneCountInString(title) > 2000 || utf8.RuneCountInString(work.Publisher) > 500 {
			continue
		}
		date, precision := formatCrossrefDate(work.Published)
		if date == "" {
			date, precision = formatCrossrefDate(work.PublishedOnline)
		}
		if date == "" {
			date, precision = formatCrossrefDate(work.PublishedPrint)
		}
		if date == "" {
			date, precision = formatCrossrefDate(work.Issued)
		}
		// A year-only/month-only record may predate the requested recency
		// window. Keep its precision visible; never invent a day.
		sum := sha256.Sum256(raw)
		items = append(items, ResearchCandidate{SourceKind: "crossref", DOI: doi, Title: title, Publisher: strings.TrimSpace(work.Publisher),
			SourceURL: "https://doi.org/" + url.PathEscape(doi), PublishedDate: date, PublishedPrecision: precision,
			SourceDigest: hex.EncodeToString(sum[:])})
	}
	return items, nil
}

func formatCrossrefDate(value crossrefDate) (string, string) {
	if len(value.DateParts) == 0 || len(value.DateParts[0]) == 0 {
		return "", ""
	}
	parts := value.DateParts[0]
	if parts[0] < 1000 || parts[0] > 9999 {
		return "", ""
	}
	if len(parts) == 1 {
		return fmt.Sprintf("%04d", parts[0]), "year"
	}
	if parts[1] < 1 || parts[1] > 12 {
		return "", ""
	}
	if len(parts) == 2 {
		return fmt.Sprintf("%04d-%02d", parts[0], parts[1]), "month"
	}
	date := time.Date(parts[0], time.Month(parts[1]), parts[2], 0, 0, 0, 0, time.UTC)
	if date.Year() != parts[0] || int(date.Month()) != parts[1] || date.Day() != parts[2] {
		return "", ""
	}
	return date.Format("2006-01-02"), "day"
}
