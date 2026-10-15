package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Opt-in finite source UI QA. The fixture serves an actual completed model/media
// report from its own source-check data; it never replaces the running app.
func TestVideoResearchActualReportAndPointerSubmit(t *testing.T) {
	base, jobID := os.Getenv("HERMETRIX_VIDEO_QA_URL"), os.Getenv("HERMETRIX_VIDEO_QA_JOB")
	if base == "" || jobID == "" {
		t.Skip("set the finite video source fixture URL and actual report job ID")
	}
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Fatal("actual Chrome required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/jobs/"+jobID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var seed struct {
		URL           string   `json:"url"`
		Question      string   `json:"question"`
		SpeechSource  string   `json:"speech_source"`
		MaxFrames     int      `json:"max_frames"`
		ResearchQuery string   `json:"research_query"`
		SourceURLs    []string `json:"source_urls"`
	}
	err = json.NewDecoder(response.Body).Decode(&seed)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("actual job seed unavailable", err)
	}
	debug, stop, err := startE2EChrome(ctx, chrome, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debug, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &e2eCDPClient{connection: conn}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.researchErrors=[];addEventListener('error',e=>researchErrors.push(e.message));addEventListener('unhandledrejection',e=>researchErrors.push(String(e.reason)));`}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": base + "/?job=" + jobID}, nil); err != nil {
		t.Fatal(err)
	}
	eval := func(js string) string {
		t.Helper()
		value, err := client.evaluate(ctx, js)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#result')&&!document.querySelector('#result').hidden?'done':''`, "done")
	if got := eval(`document.querySelector('#summary').textContent`); !strings.Contains(got, "คำพูดในคลิป") || !strings.Contains(got, "ข้อมูลค้นเพิ่ม") {
		t.Fatalf("actual report missing: %s", got)
	}
	click := func(selector string) {
		t.Helper()
		encoded, _ := json.Marshal(selector)
		var point struct{ X, Y float64 }
		if err := json.Unmarshal([]byte(eval(`(()=>{const r=document.querySelector(`+string(encoded)+`).getBoundingClientRect();return JSON.stringify({X:r.x+r.width/2,Y:r.y+r.height/2})})()`)), &point); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
			args := map[string]any{"type": kind, "x": point.X, "y": point.Y}
			if kind != "mouseMoved" {
				args["button"], args["clickCount"] = "left", 1
			}
			if err := client.call(ctx, "Input.dispatchMouseEvent", args, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, width := range []int{390, 1440} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		eval(`scrollTo(0,0);'top'`)
		if eval(`document.documentElement.scrollWidth<=innerWidth&&document.querySelector('#start').getBoundingClientRect().height>=44?'fits':''`) != "fits" {
			t.Fatalf("responsive action does not fit at %d", width)
		}
		capture := func(label string) {
			if dir := os.Getenv("HERMETRIX_VIDEO_QA_EVIDENCE"); dir != "" {
				var shot struct{ Data string }
				if err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &shot); err != nil {
					t.Fatal(err)
				}
				pixels, err := base64.StdEncoding.DecodeString(shot.Data)
				if err != nil {
					t.Fatal(err)
				}
				name := "video-" + label + "-" + map[int]string{390: "390", 1440: "1440"}[width] + ".png"
				if err := os.WriteFile(filepath.Join(dir, name), pixels, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		capture("top")
		eval(`document.querySelector('#result').scrollIntoView();'report'`)
		capture("report")
	}
	eval(`scrollTo(0,0);document.querySelector('#url').value='https://example.com/invalid-video';'invalid'`)
	click("#start")
	waitBrowserValue(t, ctx, client, `!document.querySelector('#form-error').hidden?'error':''`, "error")
	// Submit a valid request matching the actual completed source check. This
	// exercises the pointer/form/API/cache path without fabricating model output
	// or launching duplicate inference while the fixture is temporary.
	seedJSON, _ := json.Marshal(seed)
	eval(`(()=>{const seed=` + string(seedJSON) + `;document.querySelector('#url').value=seed.url;document.querySelector('#question').value=seed.question;document.querySelector('.options').open=true;document.querySelector('#speech-source').value=seed.speech_source;document.querySelector('#max-frames').value=String(seed.max_frames);document.querySelector('#research-query').value=seed.research_query;document.querySelector('#source-urls').value=seed.source_urls.join('\n');scrollTo(0,0);return 'valid'})()`)
	click("#start")
	waitBrowserValue(t, ctx, client, `document.querySelector('#form-error').hidden&&!document.querySelector('#result').hidden?'accepted':''`, "accepted")
	if got := eval(`new URL(location.href).searchParams.get('job')`); got != jobID {
		t.Fatalf("valid pointer submit did not return the real cached evidence: %s", got)
	}
	eval(`document.querySelector('#frames').scrollIntoView();'frames'`)
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('#frames img').length===2&&[...document.querySelectorAll('#frames img')].every(i=>i.complete&&i.naturalWidth>0)?'loaded':''`, "loaded")
	if got := eval(`JSON.stringify(window.researchErrors)`); got != "[]" {
		t.Fatalf("browser failures: %s", got)
	}
	t.Log("Actual report and two images rendered at390/1440; real pointer invalid submit recovers, valid submit returns actual cached evidence, no page errors")
}
