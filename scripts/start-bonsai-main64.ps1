param(
    [string]$ServerPath = 'D:\llm-server\Bonsai-demo\bin\cuda\llama-server.exe',
    [string]$ModelPath = 'D:\llm-server\Bonsai-demo\models\bonsai2-gguf\27B\Ternary-Bonsai-2-27B-PQ2_0.gguf',
    [int]$Port = 8088
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $ServerPath -PathType Leaf)) {
    throw "llama-server not found: $ServerPath"
}
if (-not (Test-Path -LiteralPath $ModelPath -PathType Leaf)) {
    throw "Bonsai model not found: $ModelPath"
}

& $ServerPath -m $ModelPath -ngl 999 -c 65536 -np 1 --fit off --jinja --host 127.0.0.1 --port $Port --cors-origins http://127.0.0.1:18734
