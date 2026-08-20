#!/bin/bash

set -e -o pipefail

if [ ! -d .git ]; then
    echo 'This script must be run from repository root' 1>&2
    echo 'Usage: bash ./scripts/bump-version.bash 1.2.3' 1>&2
    exit 1
fi

if ! git diff --quiet; then
    echo 'Working tree is dirty! Ensure all changes are committed and working tree is clean' >&2
    exit 1
fi

if ! git diff --cached --quiet; then
    echo 'Git index is dirty! Ensure all changes are committed and Git index is clean' >&2
    exit 1
fi

if [[ "$(git rev-parse --abbrev-ref HEAD)" != main ]]; then
    echo "This script must be run on 'main' branch" >&2
    exit 1
fi

version="$1"

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "The first argument must match to '^\d+\.\d+\.\d+$': ${version}" 1>&2
    echo 'Usage: bash ./scripts/bump-version.bash 1.2.3' 1>&2
    exit 1
fi

function sed_() {
    case "$OSTYPE" in
        darwin*)
            /usr/bin/sed -i '' -E "$@"
            ;;
        *)
            sed -i -E "$@"
            ;;
    esac
}

function sed_checked_() {
    local expression="$1"
    local file="$2"
    local before
    before="$(git hash-object "$file")"
    sed_ "$expression" "$file"
    if [[ "$(git hash-object "$file")" == "$before" ]]; then
        echo "Expected version replacement did not change $file" >&2
        exit 1
    fi
}

pre_commit_hook='./.pre-commit-hooks.yaml'
usage_doc='./docs/usage.md'
action_metadata='./action.yml'
install_doc='./docs/install.md'
download_script='./scripts/download-actionlint.bash'
tag="v${version}"
job_url='https://github.com/kjanat/actionlint/actions/workflows/release.yaml'
playground_html='./playground/index.html'
readme_doc='./README.md'
man_ronn='./man/actionlint.1.ronn'

echo "Bumping up version to ${version} (tag: ${tag})"

# Update container image tag in pre-commit hook (See #116 for more details)
echo "Updating $pre_commit_hook"
sed_checked_ "s/entry: ghcr\\.io\\/kjanat\\/actionlint:.*/entry: ghcr.io\\/kjanat\\/actionlint:${version}/" "$pre_commit_hook"

echo "Updating $download_script"
sed_checked_ "s/^version=\"[0-9]+\.[0-9]+\.[0-9]+\"/version=\"${version}\"/" "$download_script"

echo "Updating $usage_doc"
sed_checked_ "s/    rev: v[0-9]+\.[0-9]+\.[0-9]+/    rev: v${version}/" "$usage_doc"
sed_checked_ "s/ actionlint@[0-9]+\.[0-9]+\.[0-9]+/ actionlint@${version}/g" "$usage_doc"
sed_checked_ "s/\`ghcr\.io\/kjanat\/actionlint:[0-9]+\.[0-9]+\.[0-9]+\`/\`ghcr.io\/kjanat\/actionlint:${version}\`/g" "$usage_doc"
sed_checked_ "s/(download-actionlint\.bash\)) [0-9]+\.[0-9]+\.[0-9]+/\1 ${version}/g" "$usage_doc"

echo "Updating $action_metadata"
sed_checked_ "s/(image: docker:\/\/ghcr\.io\/kjanat\/actionlint:action-)[0-9]+\.[0-9]+\.[0-9]+/\1${version}/" "$action_metadata"

echo "Updating $install_doc"
sed_checked_ "s/(--pattern '[^']+' )v[0-9]+\.[0-9]+\.[0-9]+/\1v${version}/" "$install_doc"
sed_checked_ "s/(actionlint_)[0-9]+\.[0-9]+\.[0-9]+(_linux_amd64\.tar\.gz)/\1${version}\2/g" "$install_doc"
sed_checked_ "s/(example installs v)[0-9]+\.[0-9]+\.[0-9]+/\1${version}/" "$install_doc"
sed_checked_ "s/(\.bash\) )[0-9]+\.[0-9]+\.[0-9]+/\1${version}/" "$install_doc"

echo "Updating $playground_html"
sed_checked_ "s/kjanat\/actionlint\/releases\/tag\/v[0-9]+\.[0-9]+\.[0-9]+/kjanat\/actionlint\/releases\/tag\/v${version}/" "$playground_html"
sed_checked_ "s/id=\"version\">v[0-9]+\.[0-9]+\.[0-9]+/id=\"version\">v${version}/" "$playground_html"

for f in "$readme_doc" "$man_ronn" "$playground_html"; do
    echo "Updating document links in $f"
    sed_checked_ "s/\/kjanat\/actionlint\/blob\/v[0-9]+\.[0-9]+\.[0-9]+\/docs\//\/kjanat\/actionlint\/blob\/v${version}\/docs\//g" "$f"
done

echo 'Creating a version bump commit and a version tag'
git add "$pre_commit_hook" "$usage_doc" "$action_metadata" "$install_doc" "$download_script" "$playground_html" "$readme_doc" "$man_ronn"
git commit -m "bump up version to ${tag}"
git tag "$tag"

# This is necessary since docker/build-push-action assumes the tagged commit was also pushed to main branch
echo "Pushing bump commit to main"
git push origin main

echo "Pushing the version tag '${tag}'"
git push origin "$tag"

echo "Open release job page to check release progress ${job_url}"
if [[ "$OSTYPE" == darwin* ]]; then
    open "$job_url"
fi

echo "Update version bump timestamp"
touch .bumptimestamp

echo 'Done.'
