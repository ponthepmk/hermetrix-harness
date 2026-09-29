package product

import (
	"context"
	"fmt"
	"strings"
)

type ContentSuggestion struct {
	Platform       string   `json:"platform"`
	Destination    string   `json:"destination"`
	Body           string   `json:"body"`
	Script         string   `json:"script,omitempty"`
	Storyboard     string   `json:"storyboard,omitempty"`
	SourceClaimIDs []string `json:"source_claim_ids"`
	Method         string   `json:"method"`
}

// SuggestContentDrafts is deliberately extractive. It formats reviewed claims
// and their source links; it never asks a model to invent a new factual claim.
// The result is a temporary editor suggestion, not an approved or saved post.
func (s *Service) SuggestContentDrafts(ctx context.Context, taskID string) ([]ContentSuggestion, error) {
	workspace, err := s.GetContentWorkspace(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if workspace.Brief == nil {
		return nil, fmt.Errorf("%w: save a content brief first", ErrContentNotReady)
	}
	if len(workspace.Claims) == 0 {
		return nil, fmt.Errorf("%w: add and review at least one claim from a selected source", ErrContentNotReady)
	}
	for _, claim := range workspace.Claims {
		if !claim.Current || claim.ReviewStatus != "supported" {
			return nil, fmt.Errorf("%w: every current claim must have a supported review before suggesting copy", ErrContentNotReady)
		}
	}
	brief := workspace.Brief
	language := strings.ToLower(strings.TrimSpace(brief.Language))
	thai := strings.Contains(language, "ไทย") || strings.Contains(language, "thai")
	if !thai && language != "english" && language != "en" {
		return nil, fmt.Errorf("%w: source-grounded suggestions currently support Thai or English", ErrContentNotReady)
	}
	claims := workspace.Claims
	if len(claims) > 5 {
		claims = claims[:5]
	}
	claimIDs := []string{}
	bullets := []string{}
	sources := []string{}
	script := []string{}
	storyboard := []string{}
	for index, claim := range claims {
		number := index + 1
		claimIDs = append(claimIDs, claim.ID)
		citation := fmt.Sprintf("[%d]", number)
		if claim.Consequential {
			citation += fmt.Sprintf(" [%d-b]", number)
		}
		bullets = append(bullets, fmt.Sprintf("• %s %s", claim.Statement, citation))
		sources = append(sources, fmt.Sprintf("[%d] %s — %s", number, claim.SourceTitle, claim.SourceURL))
		if claim.Consequential {
			sources = append(sources, fmt.Sprintf("[%d-b] %s — %s", number, claim.CorroboratingSourceTitle, claim.CorroboratingSourceURL))
		}
		if thai {
			script = append(script, fmt.Sprintf("ประเด็น %d: %s (ที่มา [%d])", number, claim.Statement, number))
			storyboard = append(storyboard, fmt.Sprintf("ภาพช่วง %d: แสดงประเด็น %d และที่มา [%d]", number+1, number, number))
		} else {
			script = append(script, fmt.Sprintf("Point %d: %s (source [%d])", number, claim.Statement, number))
			storyboard = append(storyboard, fmt.Sprintf("Scene %d: show point %d and source [%d]", number+1, number, number))
		}
	}
	result := []ContentSuggestion{}
	if brief.FacebookPage != "" {
		body := ""
		if thai {
			body = fmt.Sprintf("%s\n\nสำหรับ%s มีประเด็นจากแหล่งที่ตรวจแล้ว:\n%s\n\nแหล่งข้อมูล:\n%s\n\nโปรดเปิดแหล่งต้นทางเพื่อตรวจบริบทก่อนนำไปใช้", brief.Topic, brief.Audience, strings.Join(bullets, "\n"), strings.Join(sources, "\n"))
		} else {
			body = fmt.Sprintf("%s\n\nFor %s, reviewed source claims include:\n%s\n\nSources:\n%s\n\nOpen the original sources and check context before use.", brief.Topic, brief.Audience, strings.Join(bullets, "\n"), strings.Join(sources, "\n"))
		}
		result = append(result, ContentSuggestion{Platform: "facebook", Destination: brief.FacebookPage, Body: body, SourceClaimIDs: claimIDs, Method: "reviewed-claims-template-v1"})
	}
	if brief.TikTokAccount != "" {
		body, opening, closing, first, last := "", "", "", "", ""
		if thai {
			body = fmt.Sprintf("%s · %d ประเด็นจากแหล่งที่ตรวจแล้ว ดูที่มาด้านล่าง\n%s", brief.Topic, len(claims), strings.Join(sources, "\n"))
			opening = "เปิด: " + brief.Topic
			closing = "ปิด: เปิดแหล่งต้นทางเพื่อตรวจบริบทก่อนนำไปใช้"
			first = "ภาพเปิด: ชื่อเรื่องและผู้ชม"
			last = "ภาพปิด: แสดงรายชื่อแหล่งต้นทาง"
		} else {
			body = fmt.Sprintf("%s · %d reviewed points. Sources below:\n%s", brief.Topic, len(claims), strings.Join(sources, "\n"))
			opening = "Opening: " + brief.Topic
			closing = "Closing: check the original sources before use"
			first = "Opening scene: title and audience"
			last = "Final scene: source list"
		}
		fullScript := strings.Join(append(append([]string{opening}, script...), closing), "\n")
		fullStoryboard := strings.Join(append(append([]string{first}, storyboard...), last), "\n")
		result = append(result, ContentSuggestion{Platform: "tiktok", Destination: brief.TikTokAccount, Body: body, Script: fullScript, Storyboard: fullStoryboard, SourceClaimIDs: claimIDs, Method: "reviewed-claims-template-v1"})
	}
	return result, nil
}
