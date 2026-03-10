#!/usr/bin/env bash
set -euo pipefail

vendor_root="internal/web/static/vendor"

fetch() {
  local url="$1"
  local out="$2"
  mkdir -p "$(dirname "$out")"
  curl -fL --retry 3 --retry-delay 2 -o "$out" "$url"
}

fetch "https://cdn.jsdelivr.net/npm/bootstrap@5.3.0-alpha3/dist/css/bootstrap.min.css" \
  "$vendor_root/bootstrap-5.3.0-alpha3-dist/css/bootstrap.min.css"
fetch "https://cdn.jsdelivr.net/npm/bootstrap@5.3.0-alpha3/dist/js/bootstrap.bundle.min.js" \
  "$vendor_root/bootstrap-5.3.0-alpha3-dist/js/bootstrap.bundle.min.js"

fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/bootstrap-icons.min.css" \
  "$vendor_root/bootstrap-icons-1.10.5/font/bootstrap-icons.min.css"
fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/fonts/bootstrap-icons.woff2" \
  "$vendor_root/bootstrap-icons-1.10.5/font/fonts/bootstrap-icons.woff2"
fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/fonts/bootstrap-icons.woff" \
  "$vendor_root/bootstrap-icons-1.10.5/font/fonts/bootstrap-icons.woff"

fetch "https://cdnjs.cloudflare.com/ajax/libs/cash/8.1.5/cash.min.js" \
  "$vendor_root/cash.min.js"
fetch "https://cdn.jsdelivr.net/npm/jmuxer@2.0.7/dist/jmuxer.min.js" \
  "$vendor_root/jmuxer.min.js"
fetch "https://cdn.jsdelivr.net/npm/chart.js@4.4.1/dist/chart.umd.min.js" \
  "$vendor_root/chart.umd.min.js"
