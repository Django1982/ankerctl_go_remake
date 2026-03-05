#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENTS_DIR="$ROOT_DIR/.claude/agents"
MEMORY_ROOT="$ROOT_DIR/.claude/agent-memory"
REPORTS_DIR="$ROOT_DIR/docs/agents/reports"

usage() {
    cat <<'EOF'
Usage:
  agent-context.sh --list
  agent-context.sh <agent-name> [OPTIONS]

Options:
  --list              List available agents from .claude/agents
  --task TEXT         Add task section to generated context
  --include-memory    Include MEMORY.md (default: on)
  --include-topics    Include additional markdown files from agent memory dir
  --include-claude    Include CLAUDE.md project instructions (default: on)
  --include-plan      Include MIGRATION_PLAN.md (default: off)
  --include-report FILE  Include a completion or review report for context
  --feedback FILE     Format a review report as compact Codex feedback
  -h, --help          Show this help

Examples:
  # Build context for implementation task
  agent-context.sh go-migration-architect \
    --task "Implement Phase 5 SQLite DB layer per MIGRATION_PLAN.md Phase 5"

  # Build context including migration plan
  agent-context.sh go-migration-coordinator \
    --include-plan \
    --task "Review Phase 5 status and plan Phase 6"

  # Give Codex a review report as feedback for fixes
  agent-context.sh go-migration-architect \
    --feedback docs/agents/reports/2026-03-04-phase4-review.md \
    --task "Apply the REVIEW FEEDBACK items marked FIX REQUIRED"

  # Save full context to file for Codex
  agent-context.sh go-migration-architect \
    --include-plan \
    --task "Implement Phase 5" > /tmp/codex-task.txt
EOF
}

list_agents() {
    if [[ ! -d "$AGENTS_DIR" ]]; then
        echo "No agents directory found: $AGENTS_DIR" >&2
        exit 1
    fi
    find "$AGENTS_DIR" -maxdepth 1 -type f -name "*.md" -printf "%f\n" \
        | sed 's/\.md$//' \
        | sort
}

agent_name=""
task_text=""
include_memory=1
include_topics=0
include_claude=1
include_plan=0
include_report=""
feedback_file=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --list)
            list_agents
            exit 0
            ;;
        --task)
            shift
            task_text="${1:-}"
            ;;
        --include-memory)
            include_memory=1
            ;;
        --include-topics)
            include_topics=1
            ;;
        --include-claude)
            include_claude=1
            ;;
        --include-plan)
            include_plan=1
            ;;
        --include-report)
            shift
            include_report="${1:-}"
            ;;
        --feedback)
            shift
            feedback_file="${1:-}"
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --no-memory)
            include_memory=0
            ;;
        --no-claude)
            include_claude=0
            ;;
        --*)
            echo "Unknown option: $1" >&2
            usage >&2
            exit 1
            ;;
        *)
            if [[ -z "$agent_name" ]]; then
                agent_name="$1"
            else
                echo "Unexpected argument: $1" >&2
                usage >&2
                exit 1
            fi
            ;;
    esac
    shift
done

if [[ -z "$agent_name" ]]; then
    echo "Missing agent name." >&2
    usage >&2
    exit 1
fi

agent_file="$AGENTS_DIR/$agent_name.md"
memory_dir="$MEMORY_ROOT/$agent_name"
memory_file="$memory_dir/MEMORY.md"

if [[ ! -f "$agent_file" ]]; then
    echo "Agent profile not found: $agent_file" >&2
    echo "Available agents:" >&2
    list_agents >&2
    exit 1
fi

# --- Project context (CLAUDE.md) ---
if [[ $include_claude -eq 1 && -f "$ROOT_DIR/CLAUDE.md" ]]; then
    printf '### PROJECT INSTRUCTIONS (CLAUDE.md)\n\n'
    cat "$ROOT_DIR/CLAUDE.md"
    printf '\n\n'
fi

# --- Agent profile ---
printf '### AGENT PROFILE: %s\n\n' "$agent_name"
cat "$agent_file"
printf '\n'

# --- Agent memory ---
if [[ $include_memory -eq 1 && -f "$memory_file" ]]; then
    printf '\n### AGENT MEMORY\n\n'
    cat "$memory_file"
    printf '\n'
fi

if [[ $include_topics -eq 1 && -d "$memory_dir" ]]; then
    while IFS= read -r topic_file; do
        base="$(basename "$topic_file")"
        [[ "$base" == "MEMORY.md" ]] && continue
        printf '\n### AGENT MEMORY TOPIC: %s\n\n' "$base"
        cat "$topic_file"
        printf '\n'
    done < <(find "$memory_dir" -maxdepth 1 -type f -name "*.md" | sort)
fi

# --- Migration plan ---
if [[ $include_plan -eq 1 && -f "$ROOT_DIR/MIGRATION_PLAN.md" ]]; then
    printf '\n### MIGRATION PLAN\n\n'
    cat "$ROOT_DIR/MIGRATION_PLAN.md"
    printf '\n'
fi

# --- Optional report context ---
if [[ -n "$include_report" ]]; then
    if [[ -f "$include_report" ]]; then
        printf '\n### CONTEXT REPORT: %s\n\n' "$include_report"
        cat "$include_report"
        printf '\n'
    else
        echo "Warning: report not found: $include_report" >&2
    fi
fi

# --- Feedback (compact review format for Codex iteration) ---
if [[ -n "$feedback_file" ]]; then
    if [[ -f "$feedback_file" ]]; then
        printf '\n### REVIEW FEEDBACK (apply FIX REQUIRED items)\n\n'
        cat "$feedback_file"
        printf '\n'
    else
        echo "Warning: feedback file not found: $feedback_file" >&2
    fi
fi

# --- Task ---
if [[ -n "$task_text" ]]; then
    printf '\n### TASK\n\n%s\n' "$task_text"
fi
