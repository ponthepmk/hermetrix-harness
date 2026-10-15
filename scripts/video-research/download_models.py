import argparse,concurrent.futures,hashlib,json,time,urllib.request
from pathlib import Path
parser=argparse.ArgumentParser()
parser.add_argument('--models-dir',required=True)
root=Path(parser.parse_args().models_dir).resolve()
root.mkdir(parents=True,exist_ok=True)
repos={
 'Qwen/Qwen3-VL-4B-Instruct-GGUF':{'Qwen3VL-4B-Instruct-Q4_K_M.gguf','mmproj-Qwen3VL-4B-Instruct-Q8_0.gguf'},
 'mobiuslabsgmbh/faster-whisper-large-v3-turbo':{'model.bin','config.json','tokenizer.json','preprocessor_config.json','vocabulary.json','vocabulary.txt'}}
jobs=[]
for repo,wanted in repos.items():
 with urllib.request.urlopen('https://huggingface.co/api/models/'+repo+'?blobs=true',timeout=30) as response: meta=json.load(response)
 for item in meta['siblings']:
  name=item['rfilename']
  if name not in wanted:continue
  folder=root/('qwen3-vl-4b' if repo.startswith('Qwen/') else 'whisper-turbo')
  folder.mkdir(exist_ok=True)
  jobs.append({'repo':repo,'revision':meta['sha'],'name':name,'size':item['size'],'expected_sha256':item.get('lfs',{}).get('sha256'),'path':str(folder/name)})
def download(job):
 target=Path(job['path']);expected=job['expected_sha256']
 if target.exists() and target.stat().st_size==job['size']:
  with target.open('rb') as existing: cached=hashlib.file_digest(existing,'sha256').hexdigest()
  if not expected or cached==expected:
   job['sha256']=cached;print('verified cache '+job['name'],flush=True);return job
 url=f"https://huggingface.co/{job['repo']}/resolve/{job['revision']}/{job['name']}"
 temporary=target.with_suffix(target.suffix+'.part');total=0;bucket=-1;digest=hashlib.sha256()
 with urllib.request.urlopen(url,timeout=60) as response,temporary.open('wb') as output:
  while chunk:=response.read(4*1024*1024):
   output.write(chunk);digest.update(chunk);total+=len(chunk)
   progress=int(total/max(1,job['size'])*10)
   if progress!=bucket:print(f"{job['name']}: {min(100,progress*10)}%",flush=True);bucket=progress
 if total!=job['size'] or (expected and digest.hexdigest()!=expected):raise RuntimeError('Model integrity mismatch '+job['name'])
 temporary.replace(target);job['sha256']=digest.hexdigest();return job
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
 results=list(pool.map(download,jobs))
(root/'manifest.json').write_text(json.dumps({'downloaded_at':time.time(),'models':results},indent=2),encoding='utf-8')
print('ALL MODELS VERIFIED',flush=True)
