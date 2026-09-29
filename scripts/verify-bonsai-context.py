"""Measure a local llama.cpp context window with position-specific recall facts.

Usage: python scripts/verify-bonsai-context.py --target-tokens 62000
The fixture is synthetic and never reads project files or credentials.
Use --hermetrix-session with --target-tokens 48000 to measure the application
route separately from the raw runtime route.
"""

import argparse
import json
import time
import urllib.request


FACTS = {"begin": "cedar-4817", "middle": "harbor-9362", "end": "orchid-2754"}


def post(url, payload, timeout):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    request = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8088")
    parser.add_argument("--target-tokens", type=int, default=62000)
    parser.add_argument("--max-output-tokens", type=int, default=512)
    parser.add_argument("--run-tag", default="")
    parser.add_argument("--timeout", type=int, default=900)
    parser.add_argument("--hermetrix-url", default="http://127.0.0.1:18783")
    parser.add_argument("--hermetrix-session", default="")
    args = parser.parse_args()
    base = args.base_url.rstrip("/")
    with urllib.request.urlopen(base + "/props", timeout=10) as response:
        props = json.load(response)
    allocated = props["default_generation_settings"]["n_ctx"]
    models = json.load(urllib.request.urlopen(base + "/v1/models", timeout=10))
    model = models["data"][0]["id"]
    if args.target_tokens >= allocated - args.max_output_tokens - 128:
        raise ValueError("leave room for chat framing and output")

    def prompt(lines):
        middle = lines // 2
        content = [f"Run tag: {args.run_tag}.\n", f"BEGIN PIN: {FACTS['begin']}. This is the first fact.\n"]
        for index in range(lines):
            if index == middle:
                content.append(f"MIDDLE PIN: {FACTS['middle']}. This is the middle fact.\n")
            content.append(f"Record {index:05d}: ordinary context material; no PIN appears on this record.\n")
        content.append(f"END PIN: {FACTS['end']}. This is the final fact.\n")
        content.append("Return only JSON with keys begin, middle, end and the exact PIN for each.\n")
        return "".join(content)

    def count_tokens(content):
        return len(post(base + "/tokenize", {"content": content, "add_special": False}, 120)["tokens"])

    low, high = 0, max(1, args.target_tokens // 4)
    while low < high:
        mid = (low + high + 1) // 2
        if count_tokens(prompt(mid)) <= args.target_tokens:
            low = mid
        else:
            high = mid - 1
    content = prompt(low)
    input_tokens = count_tokens(content)
    started = time.monotonic()
    if args.hermetrix_session:
        app = args.hermetrix_url.rstrip("/")
        session_url = app + "/api/sessions/" + args.hermetrix_session
        request = urllib.request.Request(
            session_url + "/turns",
            data=json.dumps({"content": content}, ensure_ascii=False).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(request, timeout=args.timeout) as response:
            response.read()
        with urllib.request.urlopen(session_url + "/events", timeout=30) as response:
            events = json.load(response)["items"]
        with urllib.request.urlopen(session_url, timeout=30) as response:
            session = json.load(response)
        completion = next((event for event in reversed(events)
                           if event["event_kind"] == "message" and event["role"] == "assistant"), {})
        answer = completion.get("content", "")
        report = (session.get("latest_context_snapshot") or {}).get("report") or {}
        print(json.dumps({
            "route": "hermetrix",
            "session_id": args.hermetrix_session,
            "profile": session["session"]["context_profile"],
            "runtime_allocated": allocated,
            "fixture_tokens": input_tokens,
            "elapsed_seconds": round(time.monotonic() - started, 2),
            "provider_usage": completion.get("metadata", {}).get("usage", {}),
            "predicted_input": report.get("predicted_input"),
            "selected_tokens": report.get("selected_tokens"),
            "dropped_tokens": report.get("dropped_tokens"),
            "answer": answer[:1500],
            "expected": FACTS,
            "all_facts_present": all(value in answer for value in FACTS.values()),
        }, ensure_ascii=False))
        if not all(value in answer for value in FACTS.values()) or report.get("dropped_tokens", 0):
            raise SystemExit(1)
        return
    result = post(base + "/v1/chat/completions", {
        "model": model,
        "messages": [{"role": "user", "content": content}],
        "temperature": 0,
        "max_tokens": args.max_output_tokens,
        "stream": False,
    }, args.timeout)
    answer = result["choices"][0]["message"].get("content", "")
    print(json.dumps({
        "runtime_allocated": allocated,
        "fixture_tokens": input_tokens,
        "lines": low,
        "elapsed_seconds": round(time.monotonic() - started, 2),
        "usage": result.get("usage", {}),
        "answer": answer,
        "expected": FACTS,
        "all_facts_present": all(value in answer for value in FACTS.values()),
    }, ensure_ascii=False))
    if not all(value in answer for value in FACTS.values()):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
