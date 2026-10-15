import argparse, json, socket, tempfile, threading, unittest, urllib.error, urllib.request
from pathlib import Path
from unittest.mock import patch
from pipeline import Jobs, video_id
from public_web import TextPage, public_target
from server import create_server
from report import render_report


class Deferred:
    def __init__(self): self.submitted = []
    def submit(self, *args): self.submitted.append(args)


class ResearchTests(unittest.TestCase):
    def test_report_binds_actual_segments_and_read_passages(self):
        import copy
        from report import web_passages
        job = {'url': 'https://youtube.com/watch?v=jNQXAC9IVRw', 'transcript': [{'start': 4.0, 'end': 5.0, 'text': 'actual spoken words'}, {'start': 8.0, 'end': 9.0, 'text': 'different words'}], 'frames': [{'time': .3}], 'sources': [{'id': 'S1', 'url': 'https://example.com', 'basis': 'web_page_text', 'text': 'This source describes the actual public event.'}, {'id': 'S2', 'url': 'https://invalid.example', 'basis': 'search_metadata'}]}
        result = {'overview': 'summary', 'spoken_points': [{'segment_id': 'T2', 'point': 'heard'}], 'visual_points': [{'time': .3, 'point': 'seen'}], 'research_findings': [{'passage_id': 'S1.P1', 'point': 'reported'}], 'limitations': ['sampled']}
        bound = copy.deepcopy(result)
        self.assertIn('&t=8', render_report(job, bound))
        self.assertEqual(bound['spoken_points'][0]['time'], 8.0)
        self.assertEqual(bound['research_findings'][0]['source_quote'], job['sources'][0]['text'])
        self.assertEqual({p['source_id'] for p in web_passages(job)}, {'S1'})
        for group, field, value in [('spoken_points', 'segment_id', 'T9'), ('research_findings', 'passage_id', 'S2.P1'), ('visual_points', 'time', 9.0), ('spoken_points', 'time', 8.0), ('spoken_points', 'point', ''), ('spoken_points', 'point', 'มี象')]:
            invalid = copy.deepcopy(result); invalid[group][0][field] = value
            with self.assertRaises(ValueError): render_report(job, invalid)
        invalid = copy.deepcopy(result); invalid['research_findings'] *= 4
        with self.assertRaises(ValueError): render_report(job, invalid)
        invalid = copy.deepcopy(result); invalid['overview'] = 'คลิปถ่ายที่สวนสัตว์ในวันที่ 23 เมษายน 2005'
        with self.assertRaises(ValueError): render_report(job, invalid)

    def test_web_passages_bound_hyphenated_quotes_and_ignore_unread_sources(self):
        import re
        from report import web_passages
        source = ' '.join(['then-private 18-second evidence'] * 30)
        passages = web_passages({'sources': [{'id': 'S1', 'text': source, 'basis': 'web_page_text'}, {'id': 'S2', 'text': 'unread metadata', 'basis': 'search_metadata'}]})
        self.assertTrue(passages)
        self.assertEqual(' '.join(p['text'] for p in passages), source)
        for passage in passages:
            self.assertEqual(passage['source_id'], 'S1')
            self.assertLessEqual(len(re.findall(r'\w+', passage['text'])), 25)
            self.assertIn(passage['text'], source)

    def test_split_caption_sentence_keeps_real_contiguous_time_range(self):
        from report import speech_evidence
        raw = [{'start': 1.2, 'end': 3.36, 'text': 'in front of elephants'}, {'start': 5.318, 'end': 7.974, 'text': 'they have really...'}, {'start': 7.974, 'end': 12.616, 'text': 'really long trunks'}, {'start': 16.881, 'end': 18.881, 'text': 'all there is to say'}]
        grouped = speech_evidence(raw)
        self.assertEqual(len(grouped), 3)
        self.assertEqual(grouped[1]['text'], 'they have really... really long trunks')
        self.assertEqual(grouped[1]['start'], 5.318)
        self.assertEqual(grouped[1]['end'], 12.616)
        self.assertEqual(grouped[1]['raw_segment_indices'], [1, 2])
        self.assertEqual(raw[1]['text'], 'they have really...')
        cues = [{'start': 1, 'end': 2, 'text': 'long trunks'}, {'start': 2, 'end': 3, 'text': '(baaaaaaaaaaahhh!!)'}, {'start': 4, 'end': 5, 'text': 'all there is to say'}]
        clean = speech_evidence(cues)
        self.assertEqual(len(clean), 2)
        self.assertNotIn('baa', ' '.join(segment['text'] for segment in clean))
        self.assertEqual(cues[1]['text'], '(baaaaaaaaaaahhh!!)')

    def test_date_repair_is_bounded_and_cannot_accept_an_invalid_repair(self):
        from report import UnverifiedFilmingDate
        bad = {'overview': 'คลิปถ่ายในปี 2005', 'spoken_points': [{'segment_id': 'T1', 'point': 'heard'}], 'visual_points': [{'time': .3, 'point': 'seen'}], 'research_findings': [{'passage_id': 'S1.P1', 'point': 'ถ่ายในวันที่ 23 เมษายน'}], 'limitations': ['sampled']}
        good = {'overview': 'คนอยู่หน้าช้าง', 'research_findings': [{'passage_id': 'S1.P1', 'point': 'แหล่งเว็บระบุการอัปโหลด'}]}
        with tempfile.TemporaryDirectory() as data:
            jobs = Jobs(data, 'http://127.0.0.1:8089', 'unused', 'unused', 'unused'); jobs.pool.shutdown()
            def evidence(): return {'url': 'https://youtube.com/watch?v=jNQXAC9IVRw', 'transcript': [{'start': 4.0, 'text': 'actual words'}], 'frames': [{'time': .3}], 'sources': [{'id': 'S1', 'url': 'https://example.com', 'basis': 'web_page_text', 'text': 'actual upload evidence'}], 'warnings': []}
            with patch.object(jobs, 'model', return_value=json.dumps(good)) as model:
                job = evidence(); jobs.finalize_report(job, json.dumps(bad)); self.assertEqual(model.call_count, 1)
                self.assertEqual(job['report_repair']['original'], bad)
                self.assertTrue(job['summary']); self.assertTrue(job['warnings'])
            with patch.object(jobs, 'model', return_value=json.dumps({'overview': bad['overview'], 'research_findings': bad['research_findings']})) as model:
                with self.assertRaises(ValueError): jobs.finalize_report(evidence(), json.dumps(bad))
                self.assertEqual(model.call_count, 1)

    def test_stream_null_content_and_truncation_preserve_honest_result(self):
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
        class Streaming(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_POST(self):
                request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                self.server.last_request = request
                self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
                for chunk in [{'choices': [{'delta': {'role': 'assistant', 'content': None}}]}, {'choices': [{'delta': {'content': 'observed'}}]}, {'choices': [{'delta': {}, 'finish_reason': 'length' if request['max_tokens'] == 1 else 'stop'}]}]:
                    self.wfile.write(('data: ' + json.dumps(chunk) + '\n\n').encode())
                self.wfile.write(b'data: [DONE]\n\n')
        server = ThreadingHTTPServer(('127.0.0.1', 0), Streaming)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            with tempfile.TemporaryDirectory() as data:
                jobs = Jobs(data, 'http://127.0.0.1:' + str(server.server_address[1]), 'unused', 'unused', 'unused', summary_url='http://127.0.0.1:' + str(server.server_address[1]))
                job = {'id': 'a' * 32}; jobs.cancelled[job['id']] = threading.Event()
                image = Path(data) / 'frame.jpg'; image.write_bytes(b'fixture')
                self.assertEqual(jobs.model(job, 'read', images=[image]), 'observed')
                self.assertEqual(jobs.model(job, 'read', images=[image], schema={'type': 'string', 'maxLength': 10, 'pattern': '^[^\u3400-\u9fff]*$'}), 'observed')
                wire_schema = server.last_request['response_format']['json_schema']['schema']
                self.assertNotIn('pattern', wire_schema)
                self.assertEqual(wire_schema['maxLength'], 10)
                with self.assertRaises(ValueError): jobs.model(job, 'read', images=[image], max_tokens=1)
                self.assertEqual(job['partial_model_output'], 'observed')
                jobs.pool.shutdown()
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_canonical_video_urls_and_rejected_targets(self):
        for url in ('https://youtu.be/jNQXAC9IVRw?t=1', 'https://www.youtube.com/watch?v=jNQXAC9IVRw', 'https://youtube.com/shorts/jNQXAC9IVRw'):
            self.assertEqual(video_id(url), 'jNQXAC9IVRw')
        for url in ('http://127.0.0.1/video', 'https://youtube.com.evil.example/watch?v=jNQXAC9IVRw', 'https://user:secret@youtube.com/watch?v=jNQXAC9IVRw', 'https://youtube.com:8443/watch?v=jNQXAC9IVRw', 'https://youtube.com/watch?v=invalid'):
            with self.assertRaises(ValueError): video_id(url)

    def test_private_and_mixed_dns_answers_are_rejected(self):
        for addresses in (['127.0.0.1'], ['10.1.2.3'], ['::1'], ['169.254.169.254'], ['8.8.8.8', '127.0.0.1']):
            resolved = [(socket.AF_INET, socket.SOCK_STREAM, 6, '', (ip, 443)) for ip in addresses]
            with patch('socket.getaddrinfo', return_value=resolved), self.assertRaises(ValueError):
                public_target('https://example.com/document')
        with self.assertRaises(ValueError): public_target('https://user:pass@example.com/')

    def test_page_content_is_plain_evidence(self):
        parser = TextPage()
        parser.feed('<title>Evidence</title><script>steal()</script><style>.secret{}</style><p>&lt;img onerror=attack()&gt; &amp; valid text</p>')
        self.assertEqual(parser.titles, ['Evidence'])
        self.assertNotIn('steal()', parser.lines)
        self.assertNotIn('.secret{}', parser.lines)
        self.assertIn('<img onerror=attack()> & valid text', parser.lines)

    def test_idempotent_start_cancel_queue_and_restart_receipts(self):
        with tempfile.TemporaryDirectory() as data:
            jobs = Jobs(data, 'http://127.0.0.1:8089', 'unused', 'unused', 'unused')
            jobs.pool.shutdown()
            jobs.pool = Deferred()
            first = jobs.start('https://youtu.be/jNQXAC9IVRw', question='หนึ่ง')
            same = jobs.start('https://youtu.be/jNQXAC9IVRw', question='หนึ่ง')
            self.assertEqual(first['id'], same['id'])
            self.assertEqual(len(jobs.pool.submitted), 1)
            jobs.start('https://youtu.be/jNQXAC9IVRw', question='สอง')
            jobs.start('https://youtu.be/jNQXAC9IVRw', question='สาม')
            with self.assertRaises(ValueError): jobs.start('https://youtu.be/jNQXAC9IVRw', question='สี่')
            cancelled = jobs.cancel(first['id'])
            self.assertEqual(cancelled['state'], 'cancelling')
            self.assertTrue(jobs.cancelled[first['id']].is_set())
            loaded = Jobs(data, 'http://127.0.0.1:8089', 'unused', 'unused', 'unused')
            self.assertEqual(loaded.get(first['id'])['state'], 'interrupted')
            loaded.pool.shutdown()

    def test_actual_http_mcp_and_boundary_errors(self):
        with tempfile.TemporaryDirectory() as data:
            options = argparse.Namespace(port=0, data=data, packages=data, whisper_model='unused', vision_url='http://127.0.0.1:8089', ffmpeg='unused', node='unused')
            server = create_server(options)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            base = 'http://127.0.0.1:' + str(server.server_address[1])
            try:
                request = urllib.request.Request(base + '/mcp', data=json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}).encode(), headers={'Content-Type': 'application/json'})
                result = json.load(urllib.request.urlopen(request))
                self.assertEqual(len(result['result']['tools']), 5)
                self.assertTrue(all(tool['annotations']['readOnlyHint'] for tool in result['result']['tools']))
                bad = urllib.request.Request(base + '/api/jobs', data=b'{"url":"https://attacker.example/"}', headers={'Content-Type': 'application/json'})
                with self.assertRaises(urllib.error.HTTPError) as error: urllib.request.urlopen(bad)
                self.assertEqual(error.exception.code, 400)
                origin = urllib.request.Request(base + '/mcp', data=b'{}', headers={'Content-Type': 'application/json', 'Origin': 'https://attacker.example'})
                with self.assertRaises(urllib.error.HTTPError) as error: urllib.request.urlopen(origin)
                self.assertEqual(error.exception.code, 403)
                with self.assertRaises(urllib.error.HTTPError) as error: urllib.request.urlopen(base + '/media/' + 'a' * 32 + '/../job.json')
                self.assertEqual(error.exception.code, 404)
                self.assertEqual(json.load(urllib.request.urlopen(base + '/api/jobs')), [])
            finally:
                server.shutdown(); server.server_close(); thread.join()


if __name__ == '__main__':
    unittest.main()
