#!/usr/bin/env sh
# release.sh <forgejo|github> <tag> — create the release and upload dist/*.
# Env: API (base URL of the API), REPO (owner/name), TOKEN.
#   forgejo: API=http://172.16.40.13:3000/api/v1  REPO=bonsai-collective/tennyn
#   github:  API=https://api.github.com           REPO=Ultizan/tennyn
set -eu
kind="${1:?forgejo|github}"; tag="${2:?tag}"
auth="Authorization: token $TOKEN"
[ "$kind" = github ] && auth="Authorization: Bearer $TOKEN"
body="$(printf '{"tag_name":"%s","name":"%s","body":"See README for usage. Assets: binaries per OS/arch, SHA256SUMS, install.sh."}' "$tag" "$tag")"
id="$(curl -fsS -X POST -H "$auth" -H 'Content-Type: application/json' -d "$body" "$API/repos/$REPO/releases" | sed -n 's/.*"id":[ ]*\([0-9]*\).*/\1/p' | head -n 1)"
[ -n "$id" ] || { echo "release.sh: no release id returned" >&2; exit 1; }
for f in dist/*; do
  name="$(basename "$f")"
  if [ "$kind" = github ]; then
    curl -fsS -X POST -H "$auth" -H 'Content-Type: application/octet-stream' --data-binary @"$f" \
      "https://uploads.github.com/repos/$REPO/releases/$id/assets?name=$name" >/dev/null
  else
    curl -fsS -X POST -H "$auth" -F "attachment=@$f" "$API/repos/$REPO/releases/$id/assets?name=$name" >/dev/null
  fi
  echo "uploaded $name to $kind"
done
