#!/bin/zsh
set -eu
project_dir="$(cd "$(dirname "$0")" && pwd)"
open "$project_dir/docs-site/index.html"
