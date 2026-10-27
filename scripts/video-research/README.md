# Video Research operator guide

This selected project is ready to research public YouTube videos. Use its
entrypoint; do not inspect source code, list historical .research-jobs, or
read raw job.json files to plan a new request. The CLI selects its matching
cache and returns the actual result. No installation is needed on this machine.

1. Start ONE workspace.run command. Example below: replace URL/question/source
   with the owner's request. Omit --source-url when absent. Use audio when
   requested; 2 frames for a short check, 6 by default.

```json
{"action":"start","executable":"node","arguments":["cli.mjs","--url","USER_YOUTUBE_URL","--question","USER_QUESTION","--speech-source","audio","--max-frames","2","--source-url","USER_SOURCE_URL"],"timeout_seconds":600}
```

2. Poll workspace.run with {"action":"status","job_id":"RETURNED_JOB_ID"}.
   It waits up to 30 seconds. Repeat status if running; never repeat start.
3. Require completed, exit0 and verified_result:true. Read the returned
   report_file with workspace.read_file, then display that report Markdown
   directly with timestamps, sources and limitations. Do not add guessed facts
   or a second paraphrase. Failed receipts are not completed research.

Whisper transcribes audio; Qwen reads sampled images and writes the report.
Bonsai controls these tools with thinking disabled in the verified source.
Images are samples, not every frame. Audio/caption differences are retained.
Setup, limits and pending live activation: [SETUP.md](SETUP.md). Read setup
only for a setup/runtime failure.
