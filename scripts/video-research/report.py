"""Bounded structured reports; citations are rendered from actual evidence."""
import re


class UnverifiedFilmingDate(ValueError):
    pass


def speech_evidence(segments):
    grouped = []
    for index, segment in enumerate(segments):
        text = re.sub(r'\s+', ' ', segment['text']).strip()
        if re.fullmatch(r'\([^)]{1,200}\)|\[[^]]{1,200}\]', text):
            # A complete parenthesized caption is a stage/sound cue, not
            # verified human speech. Raw captions remain available for audit.
            continue
        # Captions often split a single sentence across consecutive entries.
        # Bind one contiguous evidence range rather than pretending the first
        # half contains the noun spoken in the second half.
        if grouped and segment['start'] - grouped[-1]['end'] <= .5 and len(grouped[-1]['text']) + len(text) < 400:
            grouped[-1]['text'] += ' ' + text
            grouped[-1]['end'] = segment.get('end', segment['start'])
            grouped[-1]['raw_segment_indices'].append(index)
        else:
            grouped.append({'start': segment['start'], 'end': segment.get('end', segment['start']), 'text': text, 'raw_segment_indices': [index]})
    return grouped


def web_passages(job):
    passages = []
    for source in job['sources']:
        if source.get('basis') != 'web_page_text':
            continue
        words = re.sub(r'\s+', ' ', source.get('text', '')).strip().split(' ')
        current, count = [], 0
        def append_passage():
            if current:
                passages.append({'passage_id': source['id'] + '.P' + str(1 + sum(p['source_id'] == source['id'] for p in passages)), 'source_id': source['id'], 'text': ' '.join(current)})
        for word in words:
            word_count = len(re.findall(r'\w+', word))
            if current and count + word_count > 25:
                append_passage(); current, count = [], 0
            if word_count > 25:
                continue
            current.append(word); count += word_count
        append_passage()
    return passages


def validate_shape(value, schema):
    kind = schema['type']
    valid = {'object': isinstance(value, dict), 'array': isinstance(value, list), 'string': isinstance(value, str), 'number': isinstance(value, (int, float)) and not isinstance(value, bool)}
    if not valid.get(kind, False):
        raise ValueError('รายงานใช้ชนิดข้อมูลไม่ตรงกับรูปแบบที่ตรวจได้')
    if 'enum' in schema and value not in schema['enum']:
        raise ValueError('รายงานอ้างหลักฐานที่ไม่มีอยู่จริง')
    if kind == 'object':
        if set(value) != set(schema['required']):
            raise ValueError('รายงานมีส่วนที่ขาดหรือเกินจากรูปแบบที่ตรวจได้')
        for name, child in schema['properties'].items():
            validate_shape(value[name], child)
    elif kind == 'array':
        if not schema.get('minItems', 0) <= len(value) <= schema.get('maxItems', len(value)):
            raise ValueError('จำนวนส่วนของรายงานเกินขอบเขตที่ตรวจได้')
        for child in value:
            validate_shape(child, schema['items'])
    elif kind == 'string':
        if len(value) > schema.get('maxLength', len(value)) or not value.strip() or ('pattern' in schema and not re.fullmatch(schema['pattern'], value)):
            raise ValueError('ข้อความรายงานว่าง ยาวเกินขอบเขต หรือไม่เป็นภาษาไทยตามที่ขอ')


def report_schema(job):
    def thai_text(limit):
        return {'type': 'string', 'maxLength': limit, 'pattern': '^[^\u3400-\u9fff]*$'}
    def point(times, speech=False):
        properties = {'time': {'type': 'number', 'enum': times}, 'point': thai_text(220)}
        required = ['time', 'point']
        if speech:
            properties['source_quote'] = {'type': 'string', 'maxLength': 160}
            required.append('source_quote')
        return {'type': 'object', 'properties': properties, 'required': required, 'additionalProperties': False}
    speech = job.get('speech_evidence', job.get('summary_transcript', job['transcript']))
    sources = [s for s in job['sources'] if s.get('basis') == 'web_page_text']
    findings = {'type': 'array', 'maxItems': min(3, len(sources)), 'items': {'type': 'object', 'properties': {'passage_id': {'type': 'string', 'enum': [p['passage_id'] for p in web_passages(job)] or ['none']}, 'point': thai_text(220)}, 'required': ['passage_id', 'point'], 'additionalProperties': False}}
    return {'type': 'object', 'properties': {
        'overview': thai_text(300),
        'spoken_points': {'type': 'array', 'minItems': min(1, len(speech)), 'maxItems': min(3, len(speech)), 'items': {'type': 'object', 'properties': {'segment_id': {'type': 'string', 'enum': ['T' + str(index + 1) for index in range(len(speech))] or ['none']}, 'point': thai_text(220)}, 'required': ['segment_id', 'point'], 'additionalProperties': False}},
        'visual_points': {'type': 'array', 'minItems': min(1, len(job['frames'])), 'maxItems': min(2, len(job['frames'])), 'items': point([f['time'] for f in job['frames']] or [0])},
        'research_findings': findings,
        'limitations': {'type': 'array', 'minItems': 1, 'maxItems': 3, 'items': thai_text(220)}
    }, 'required': ['overview', 'spoken_points', 'visual_points', 'research_findings', 'limitations'], 'additionalProperties': False}


def render_report(job, report):
    validate_shape(report, report_schema(job))
    displayed = [report['overview'], *report['limitations'], *(p['point'] for key in ('spoken_points', 'visual_points', 'research_findings') for p in report[key])]
    if any(re.search('[\u3400-\u9fff]', value) for value in displayed):
        raise ValueError('รายงานภาษาไทยมีอักษรจีนปน ยังไม่รับเป็นผลสรุปสมบูรณ์')
    if any(re.search(r'ถ่าย[^\n.!?]{0,70}(?:วันที่|ในปี|เมื่อปี)', value) for value in displayed):
        raise UnverifiedFilmingDate('รายงานอ้างวันถ่ายที่ระบบยังไม่ได้ยืนยัน แยกวันอัปโหลดจากวันถ่ายก่อน')
    # The compact overview is a projection of the evidence-bound points,
    # not an additional free narrative that can invent emotion or chronology.
    primary = report['spoken_points'] or report['visual_points']
    if primary:
        overview = primary[0]['point']
        if len(primary) > 1 and len(overview) + len(primary[1]['point']) + 1 <= 300:
            overview += ' ' + primary[1]['point']
        report['overview'] = overview
    lines = [report['overview']]
    for title, key, times in [('คำพูดในคลิป', 'spoken_points', [s['start'] for s in job.get('speech_evidence', job.get('summary_transcript', job['transcript']))]), ('ภาพที่ตรวจจริง', 'visual_points', [f['time'] for f in job['frames']])]:
        lines.append('\n### ' + title)
        for point in report[key]:
            if key == 'spoken_points':
                segments = job.get('speech_evidence', job.get('summary_transcript', job['transcript']))
                references = {'T' + str(index + 1): segment for index, segment in enumerate(segments)}
                if point.get('segment_id') not in references:
                    raise ValueError('รายงานอ้างช่วงคำพูดที่ไม่มีหลักฐาน')
                segment = references[point['segment_id']]
                # The model selects an actual segment. Provenance comes from
                # that segment, never from a generated quote or timestamp.
                point['time'] = segment['start']
                point['end'] = segment.get('end', segment['start'])
            elif point['time'] not in times:
                raise ValueError('รายงานอ้างเวลาที่ไม่มีหลักฐาน')
            seconds = int(point['time'])
            stamp = f'{seconds // 60}:{seconds % 60:02d}'
            lines.append(f"- [{stamp}]({job['url']}&t={seconds}) {point['point']}")
    lines.append('\n### ข้อมูลค้นเพิ่ม')
    sources = {s['id']: s for s in job['sources'] if s.get('basis') == 'web_page_text'}
    passages = {p['passage_id']: p for p in web_passages(job)}
    for point in report['research_findings']:
        if point['passage_id'] not in passages:
            raise ValueError('รายงานอ้างแหล่งที่ยังอ่านไม่สำเร็จ')
        passage = passages[point['passage_id']]
        point['source_id'] = passage['source_id']
        point['source_quote'] = passage['text']
        source = sources[point['source_id']]
        quote = point.get('source_quote', '')
        normalized = lambda value: re.sub(r'\s+', ' ', value).strip()
        if not quote or len(re.findall(r'\w+', quote)) > 25 or normalized(quote) not in normalized(source.get('text', '')):
            raise ValueError('ข้อความที่ใช้อ้างอิงไม่ตรงกับหน้าเว็บที่อ่านจริง')
        lines.append(f"- [{source['id']}]({source['url']}) {point['point']}")
    if not report['research_findings']:
        lines.append('- ยังไม่มีข้อมูลจากภายนอกที่ยืนยันได้จากหลักฐานชุดนี้')
    lines.append('\n### ข้อจำกัด')
    lines.extend('- ' + value for value in report['limitations'])
    lines.append(f"- ตรวจภาพตัวอย่าง {len(job['frames'])} ช่วงเวลา ไม่ใช่ทุกเฟรมของวิดีโอ")
    return '\n'.join(lines)
