"""Finite owned-command entrypoint for Hermetrix's existing workspace.run."""
import argparse, json, os, shutil, sys, time
from pathlib import Path

sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')
source = Path(__file__).resolve().parent
root = source.parents[1]
sys.path.insert(0, str(root / '.tmp-ui/youtube-research-20261013/python-packages'))
from pipeline import Jobs

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--question', default='')
parser.add_argument('--speech-source', choices=['auto', 'audio'], default='auto')
parser.add_argument('--max-frames', type=int, default=6)
parser.add_argument('--research-query', default='')
parser.add_argument('--source-url', action='append', default=[])
options = parser.parse_args()
data = source / '.research-jobs'
data.mkdir(exist_ok=True)
# One owned CLI worker per project. A second process must not mark the first
# worker's durable job as interrupted while it is still running.
lock = (data / '.cli.lock').open('a+b')
if lock.tell() == 0:
    lock.write(b'0'); lock.flush()
started_wait = time.monotonic()
while True:
    try:
        lock.seek(0)
        if os.name == 'nt':
            import msvcrt
            msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
        else:
            import fcntl
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        break
    except (OSError, BlockingIOError):
        if time.monotonic() - started_wait > 60:
            raise SystemExit('Another research command is still running in this project; check its owned job before starting again')
        time.sleep(1)
models = Path(os.environ.get('HERMETRIX_RESEARCH_MODEL_DIR', 'D:/llm-server/Hermetrix-research/models'))
node = os.environ.get('HERMETRIX_RESEARCH_NODE') or shutil.which('node')
ffmpeg = shutil.which('ffmpeg')
if not node or not ffmpeg:
    raise SystemExit('Node and FFmpeg are required; run the setup launcher first')
jobs = Jobs(data, 'http://127.0.0.1:8089', str(models / 'whisper-turbo'), ffmpeg, node)
job = jobs.start(options.url, question=options.question, speech_source=options.speech_source, max_frames=options.max_frames, research_query=options.research_query, source_urls=options.source_url)
previous = None
last_progress = 0
while True:
    job = jobs.get(job['id'])
    progress = (job['state'], job['stage'], job['progress'])
    substantive = progress[0] != (previous[0] if previous else None) or progress[2] // 20 != (previous[2] // 20 if previous else -1)
    if progress != previous and (substantive or time.monotonic() - last_progress > 30):
        print(json.dumps({'job_id': job['id'], 'state': job['state'], 'stage': job['stage'], 'progress': job['progress']}, ensure_ascii=False), flush=True)
        previous = progress
        last_progress = time.monotonic()
    if job['state'] == 'completed':
        report = source / '.research-jobs' / job['id'] / 'report.md'
        report.write_text(job['summary'], encoding='utf-8')
        print(json.dumps({'verified_result': True, 'report_file': str(report.relative_to(source)), 'transcript_basis': job.get('transcript_basis'), 'frames_read': len(job['frames']), 'sources_read': sum(item.get('basis') == 'web_page_text' for item in job['sources']), 'warnings_count': len(job['warnings'])}, ensure_ascii=False), flush=True)
        break
    if job['state'] in ('failed', 'cancelled', 'interrupted'):
        print(json.dumps({'verified_result': False, 'error': job.get('error'), 'evidence_file': str(source / '.research-jobs' / job['id'] / 'job.json')}, ensure_ascii=False), flush=True)
        raise SystemExit(1)
    time.sleep(1)
