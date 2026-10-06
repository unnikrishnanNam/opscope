#!/usr/bin/env bash
# Checks that deploy/kubernetes/opscope.yaml uses the image being released,
# so the manifest in a release tag always runs that release:
#
#   .github/scripts/check-manifest-tag.sh v1.2.0
#
# release.yml runs it before building. It passes without checking for:
#   - a pre-release (v1.3.0-rc.1): the manifest stays on the last real release
#   - a tag whose manifest doesn't use the published image yet (v1.0.0 and
#     v1.1.0 used opscope:dev), so those can still be published again by hand
set -euo pipefail

tag="${1:?usage: $0 <tag>, e.g. v1.2.0}"
manifest="${2:-deploy/kubernetes/opscope.yaml}"

if [[ "$tag" == *-* ]]; then
  echo "$tag is a pre-release; the manifest isn't checked."
  exit 0
fi

# The Deployment's image line, e.g. "image: ghcr.io/owner/opscope:v1.2.0".
# Comment lines start with "#", so they don't match.
image=$(grep -E '^[[:space:]]*image:' "$manifest" | head -1 | sed -E 's/^[[:space:]]*image:[[:space:]]*//; s/[[:space:]]*(#.*)?$//')

if [[ "$image" != ghcr.io/*/opscope:* ]]; then
  echo "::warning file=$manifest::It doesn't use the published image ($image), so its tag isn't checked."
  exit 0
fi

if [[ "${image##*:}" != "$tag" ]]; then
  echo "::error file=$manifest::It uses ${image##*:}, but this release is $tag. Change the image tag to $tag, merge that, then tag again."
  exit 1
fi

echo "The manifest uses $image."
