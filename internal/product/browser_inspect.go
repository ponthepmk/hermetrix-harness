package product

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"hermetrix-harness/internal/agentruntime"
)

// inspectBrowserTab reads only state already present in the current page. It
// does not fetch scripts, submit forms, follow links or probe candidate APIs.
func (s *Service) inspectBrowserTab(ctx context.Context, tab *browserTabRuntime) (agentruntime.BrowserInspection, error) {
	const expression = `(() => {
const root=document.documentElement.cloneNode(true);
for(const el of root.querySelectorAll('input,textarea')) { el.removeAttribute('value'); if(el.tagName==='TEXTAREA') el.textContent=''; }
for(const el of root.querySelectorAll('script')) el.textContent='';
for(const el of root.querySelectorAll('[href],[src],[action]')) { for(const name of ['href','src','action']) el.removeAttribute(name); }
const scripts=[...document.scripts].slice(0,40).map(el=>({url:el.src||'',inline_excerpt:el.src?'':(el.textContent||'').slice(0,2048)}));
const forms=[...document.forms].slice(0,30).map(el=>({method:(el.method||'get').toUpperCase(),action:el.action||location.href,has_password:!!el.querySelector('input[type=password]')}));
const resources=performance.getEntriesByType('resource').slice(-120).map(el=>({url:el.name||'',initiator_type:el.initiatorType||'other',status:el.responseStatus||0}));
const routes=[...document.querySelectorAll('a[href]')].slice(0,200).map(el=>el.href);
return JSON.stringify({page_url:location.href,dom_excerpt:root.outerHTML.slice(0,16384),scripts,forms,resources,routes});
})()`
	value, err := tab.client.evaluate(ctx, expression)
	if err != nil {
		return agentruntime.BrowserInspection{}, err
	}
	var raw struct {
		PageURL    string                                   `json:"page_url"`
		DOMExcerpt string                                   `json:"dom_excerpt"`
		Scripts    []agentruntime.BrowserInspectionScript   `json:"scripts"`
		Forms      []agentruntime.BrowserInspectionForm     `json:"forms"`
		Resources  []agentruntime.BrowserInspectionResource `json:"resources"`
		Routes     []string                                 `json:"routes"`
	}
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return agentruntime.BrowserInspection{}, fmt.Errorf("decode browser inspection: %w", err)
	}
	if _, err := s.validateBrowserURL(ctx, tab.tab.ProjectID, raw.PageURL, tab.tab.AllowPrivate); err != nil {
		return agentruntime.BrowserInspection{}, &browserRequestPolicyError{URL: raw.PageURL, Cause: err}
	}
	pageURL, _ := url.Parse(raw.PageURL)
	out := agentruntime.BrowserInspection{DOMExcerpt: raw.DOMExcerpt,
		Scripts:    make([]agentruntime.BrowserInspectionScript, 0, len(raw.Scripts)),
		Forms:      make([]agentruntime.BrowserInspectionForm, 0, len(raw.Forms)),
		Resources:  make([]agentruntime.BrowserInspectionResource, 0, len(raw.Resources)),
		Routes:     []string{},
		Traffic:    tab.client.snapshotTraffic(),
		PageErrors: tab.client.snapshotPageErrors()}
	if len(out.DOMExcerpt) > 16<<10 {
		out.DOMExcerpt = out.DOMExcerpt[:16<<10]
	}
	inlineRemaining := 8 << 10
	for _, script := range raw.Scripts {
		if len(out.Scripts) == 40 {
			break
		}
		script.URL = inspectionURL(script.URL)
		if len(script.InlineExcerpt) > inlineRemaining {
			script.InlineExcerpt = script.InlineExcerpt[:inlineRemaining]
		}
		inlineRemaining -= len(script.InlineExcerpt)
		if script.URL != "" || script.InlineExcerpt != "" {
			out.Scripts = append(out.Scripts, script)
		}
	}
	for _, form := range raw.Forms {
		if len(out.Forms) == 30 {
			break
		}
		form.Action = inspectionURL(form.Action)
		if form.Action == "" {
			continue
		}
		if form.Method != "POST" {
			form.Method = "GET"
		}
		out.Forms = append(out.Forms, form)
	}
	for _, resource := range raw.Resources {
		if len(out.Resources) == 120 {
			break
		}
		resource.URL = inspectionURL(resource.URL)
		if resource.URL == "" {
			continue
		}
		if len(resource.InitiatorType) > 32 {
			resource.InitiatorType = resource.InitiatorType[:32]
		}
		if resource.Status < 100 || resource.Status > 599 {
			resource.Status = 0
		}
		out.Resources = append(out.Resources, resource)
	}
	seen := map[string]bool{}
	for _, route := range raw.Routes {
		if len(out.Routes) == 80 {
			break
		}
		normalized := inspectionURL(route)
		parsed, err := url.Parse(normalized)
		if err != nil || !strings.EqualFold(parsed.Host, pageURL.Host) || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out.Routes = append(out.Routes, normalized)
	}
	return out, nil
}

// inspectionURL omits query values, fragments and userinfo, which can carry
// credentials. The path is retained so routes and API calls remain useful.
func inspectionURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	normalized := parsed.String()
	if len(normalized) > 512 {
		return normalized[:512]
	}
	return normalized
}
