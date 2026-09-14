#!/usr/bin/env bash
# Benchmark a mangarr-upscaler worker on your hardware (the "S1 spike").
#
#   scripts/upscale-bench.sh <worker-url> <token> <chapter.cbz> [pages]
#
# Sends the first N pages of a CBZ to every model at 2x and prints seconds
# per page, output size and resolution, so you can pick default models for
# your profiles. Needs: curl, unzip, zip, python3.
set -euo pipefail
url=${1:?worker url, e.g. http://localhost:8788}
token=${2:-}
cbz=${3:?path to a chapter .cbz}
pages=${4:-4}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
unzip -q -o "$cbz" -d "$work/src"
mkdir -p "$work/in"
find "$work/src" -maxdepth 1 -type f \( -iname '*.jpg' -o -iname '*.jpeg' -o -iname '*.png' -o -iname '*.webp' \) | sort | head -n "$pages" | while read -r f; do
  cp "$f" "$work/in/"
done
n=$(ls "$work/in" | wc -l | tr -d ' ')
(cd "$work/in" && zip -q -0 ../in.zip ./*)
auth=()
[ -n "$token" ] && auth=(-H "Authorization: Bearer $token")

echo "worker: $url"
curl -fsS "${auth[@]}" "$url/v1/info" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("devices:", ", ".join(d["devices"]) or "unknown"); print("models:", ", ".join(m["name"] for m in d["models"]))'
echo "pages: $n from $(basename "$cbz")"
printf '%-26s %10s %12s %12s\n' model "s/page" "out MB" "first page"
for model in $(curl -fsS "${auth[@]}" "$url/v1/info" | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin)["models"] if 2 in m["scales"]))'); do
  start=$(python3 -c 'import time; print(time.time())')
  if curl -fsS "${auth[@]}" --data-binary @"$work/in.zip" -o "$work/out-$model.zip" "$url/v1/upscale?model=$model&scale=2&noise=1&format=webp&quality=90"; then
    end=$(python3 -c 'import time; print(time.time())')
    python3 - "$work/out-$model.zip" "$start" "$end" "$n" "$model" <<'EOF'
import sys, zipfile, os, struct
path, start, end, n, model = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
z = zipfile.ZipFile(path)
first = z.read(z.namelist()[0])
dim = "?"
if first[:4] == b"RIFF" and first[12:16] == b"VP8 ":
    w, h = struct.unpack("<HH", first[26:30]); dim = f"{w & 0x3fff}x{h & 0x3fff}"
print(f"{model:<26} {(end-start)/n:10.2f} {os.path.getsize(path)/1e6:12.1f} {dim:>12}")
EOF
  else
    printf '%-26s %10s\n' "$model" failed
  fi
done
