"""Bounded public web evidence, with DNS-pinned connections and no credentials."""
import hashlib, html, http.client, ipaddress, json, re, socket, ssl
import urllib.parse, xml.etree.ElementTree as ET
from datetime import datetime, timezone
from html.parser import HTMLParser


def now():
    return datetime.now(timezone.utc).isoformat()


def public_target(raw):
    parsed = urllib.parse.urlsplit(raw)
    if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError('ต้องเป็นลิงก์เว็บสาธารณะ http/https ที่ไม่มีข้อมูลเข้าสู่ระบบ')
    port = parsed.port or (443 if parsed.scheme == 'https' else 80)
    if port != (443 if parsed.scheme == 'https' else 80):
        raise ValueError('รองรับเฉพาะพอร์ตเว็บมาตรฐาน')
    addresses = socket.getaddrinfo(parsed.hostname, port, type=socket.SOCK_STREAM)
    if not addresses or any(not ipaddress.ip_address(item[4][0]).is_global for item in addresses):
        raise ValueError('เครื่องมือค้นคว้าอ่านได้เฉพาะเว็บสาธารณะ')
    return parsed, addresses[0][4][0], port


def fetch(raw, limit=2 * 1024 * 1024):
    for _ in range(5):
        parsed, address, port = public_target(raw)
        connection = http.client.HTTPConnection(parsed.hostname, port, timeout=15)
        # Pin the resolved public address; do not resolve a second time or use
        # environment proxies, cookies, browser sessions or ambient credentials.
        transport = socket.create_connection((address, port), timeout=15)
        if parsed.scheme == 'https':
            transport = ssl.create_default_context().wrap_socket(transport, server_hostname=parsed.hostname)
        connection.sock = transport
        try:
            target = urllib.parse.urlunsplit(('', '', parsed.path or '/', parsed.query, ''))
            connection.request('GET', target, headers={'User-Agent': 'HermetrixResearch/1.0', 'Accept-Encoding': 'identity', 'Accept': 'text/html,application/xml,text/plain'})
            response = connection.getresponse()
            if response.status in (301, 302, 303, 307, 308):
                location = response.getheader('Location')
                if not location:
                    raise ValueError('เว็บส่ง redirect ที่ไม่มีปลายทาง')
                raw = urllib.parse.urljoin(raw, location)
                continue
            if response.status != 200:
                raise ValueError(f'เว็บตอบ HTTP {response.status}')
            content_type = response.getheader('Content-Type', '')
            if not any(kind in content_type for kind in ('text/', 'xml', 'json')):
                raise ValueError('หน้านี้ไม่ใช่เอกสารข้อความที่อ่านได้')
            body = response.read(limit + 1)
            if len(body) > limit:
                raise ValueError('หน้าเว็บใหญ่เกินขอบเขตการอ่าน 2 MB')
            return raw, body, content_type
        finally:
            connection.close()
    raise ValueError('เว็บเปลี่ยนปลายทางหลายครั้งเกินไป')


class TextPage(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.skip = 0
        self.title = False
        self.titles, self.lines = [], []

    def handle_starttag(self, tag, attrs):
        if tag in ('script', 'style', 'noscript', 'svg'):
            self.skip += 1
        if tag == 'title':
            self.title = True

    def handle_endtag(self, tag):
        if tag in ('script', 'style', 'noscript', 'svg'):
            self.skip = max(0, self.skip - 1)
        if tag == 'title':
            self.title = False

    def handle_data(self, data):
        if not self.skip and data.strip():
            self.lines.append(data.strip())
            if self.title:
                self.titles.append(data.strip())


def read_page(url, max_chars=8000):
    final, body, content_type = fetch(url)
    parser = TextPage()
    parser.feed(body.decode('utf-8', errors='replace'))
    text = '\n'.join(parser.lines)
    return {'url': final, 'title': ' '.join(parser.titles)[:300], 'text': text[:max_chars], 'truncated': len(text) > max_chars, 'retrieved_at': now(), 'sha256': hashlib.sha256(body).hexdigest(), 'basis': 'web_page_text', 'untrusted': True}


def search_web(query, limit=5):
    query = query.strip()
    if not 2 <= len(query) <= 240:
        raise ValueError('คำค้นต้องยาว 2–240 ตัวอักษร')
    _, body, _ = fetch('https://www.bing.com/search?' + urllib.parse.urlencode({'q': query, 'format': 'rss'}))
    root = ET.fromstring(body)
    results = []
    sites = re.findall(r'\bsite:([A-Za-z0-9.-]+)', query.lower())
    terms = set(re.findall(r'[a-z]{3,}', re.sub(r'\bsite:\S+', '', query.lower()))) - {'the', 'and', 'for', 'with', 'from'}
    for item in root.findall('./channel/item'):
        link = item.findtext('link', '')
        parsed = urllib.parse.urlsplit(link)
        if parsed.scheme not in ('http', 'https') or not parsed.hostname:
            continue
        if sites and not any(parsed.hostname == site or parsed.hostname.endswith('.' + site) for site in sites):
            continue
        searchable = (item.findtext('title', '') + ' ' + item.findtext('description', '')).lower()
        if terms and sum(term in searchable for term in terms) < max(1, min(2, len(terms))):
            continue
        results.append({'title': html.unescape(item.findtext('title', '')), 'url': link, 'snippet': html.unescape(item.findtext('description', '')), 'basis': 'search_metadata', 'retrieved_at': now()})
        if len(results) >= min(8, max(1, limit)):
            break
    if not results and not sites:
        # Bibliographic fallback, clearly labeled. This is not a claim that a
        # general web search succeeded or that Wikipedia is a primary source.
        try:
            endpoint = 'https://en.wikipedia.org/w/api.php?' + urllib.parse.urlencode({'action': 'opensearch', 'search': query, 'limit': min(8, limit), 'namespace': 0, 'format': 'json'})
            _, document, _ = fetch(endpoint)
            items = json.loads(document)
            for title, snippet, link in zip(items[1], items[2], items[3]):
                results.append({'title': title, 'url': link, 'snippet': snippet, 'basis': 'search_metadata', 'search_provider': 'Wikipedia open search fallback', 'retrieved_at': now()})
        except (ValueError, KeyError, IndexError):
            pass
    return results
