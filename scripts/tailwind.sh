#!/bin/bash
# Compile TailwindCSS
# Usage: ./scripts/tailwind.sh [--watch]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

INPUT="$PROJECT_ROOT/static/css/input.css"
OUTPUT="$PROJECT_ROOT/static/css/output.css"

# Check if tailwindcss is installed
if ! command -v tailwindcss &> /dev/null; then
    echo "Error: tailwindcss CLI not found"
    echo "Install it from: https://github.com/tailwindlabs/tailwindcss/releases"
    exit 1
fi

if [ "$1" == "--watch" ]; then
    echo "Watching for changes..."
    tailwindcss -i "$INPUT" -o "$OUTPUT" --watch
else
    echo "Building CSS..."
    tailwindcss -i "$INPUT" -o "$OUTPUT" --minify
    echo "Done: $OUTPUT"
fi
