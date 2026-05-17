<script lang="ts">
  import type { Message } from "../../api/types.js";
  import type { CallTiming, TurnTiming } from "../../api/types/timing.js";
  import { formatTimestamp } from "../../utils/format.js";
  import { formatDuration } from "../../utils/duration.js";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import { formatMessageForCopy } from "../../utils/copy-message.js";
  import {
    parseContent,
    enrichSegments,
  } from "../../utils/content-parser.js";
  import { sessionTiming } from "../../stores/sessionTiming.svelte.js";
  import { liveTick } from "../../stores/liveTick.svelte.js";
  import ToolBlock from "./ToolBlock.svelte";
  import ParallelGroup from "./ParallelGroup.svelte";
  import CopyButton from "../shared/CopyButton.svelte";
  import { displayToolName } from "../../utils/toolDisplay.js";

  interface Props {
    messages: Message[];
    timestamp: string;
    highlightQuery?: string;
    isCurrentHighlight?: boolean;
  }

  let {
    messages,
    timestamp,
    highlightQuery = "",
    isCurrentHighlight = false,
  }: Props = $props();

  let copied = $state(false);

  /** Effective tool-call count for one message: structured
   *  `tool_calls` when present, falling back to parsed tool
   *  segments so legacy transcripts (e.g. `[Bash]...` markers
   *  with no structured calls) still match the rendering path. */
  function messageToolCount(m: Message): number {
    const structured = m.tool_calls?.length ?? 0;
    if (structured > 0) return structured;
    return enrichSegments(
      parseContent(m.content, m.has_tool_use, m.id, m.content_length),
      m.tool_calls,
    ).filter((s) => s.type === "tool").length;
  }

  let totalCalls = $derived(
    messages.reduce((n, m) => n + messageToolCount(m), 0),
  );

  let label = $derived(
    totalCalls === 1 ? "1 tool call" : `${totalCalls} tool calls`,
  );

  /** Index turn timings by message id for O(1) lookup. */
  let turnByMessage = $derived.by(() => {
    const m = new Map<number, TurnTiming>();
    for (const t of sessionTiming.timing?.turns ?? []) {
      m.set(t.message_id, t);
    }
    return m;
  });

  /** Index call timings by tool_use_id for O(1) lookup. */
  let callByToolUseID = $derived.by(() => {
    const m = new Map<string, CallTiming>();
    for (const t of sessionTiming.timing?.turns ?? []) {
      for (const c of t.calls) m.set(c.tool_use_id, c);
    }
    return m;
  });

  /** Resolve the duration badge for a solo (non-grouped) tool call.
   *  Sub-agent calls show their exact duration; non-sub-agent solo
   *  calls inherit the turn's wall-clock duration. Running turns
   *  synthesize a live `running …+` label from the turn's
   *  `started_at`, ticked once per second by `liveTick`. */
  function soloDurationLabel(
    ct: CallTiming | undefined,
    turn: TurnTiming | undefined,
    msg: Message,
  ): string | undefined {
    if (ct?.subagent_session_id && ct.duration_ms != null) {
      return formatDuration(ct.duration_ms);
    }
    if (turn?.duration_ms != null) {
      return formatDuration(turn.duration_ms);
    }
    if (sessionTiming.timing?.running && turn != null) {
      const startSrc = turn.started_at ?? msg.timestamp;
      const startMs = new Date(startSrc).getTime();
      const elapsed = Number.isNaN(startMs)
        ? 0
        : Math.max(0, liveTick.now - startMs);
      return `running ${formatDuration(elapsed)}+`;
    }
    return undefined;
  }

  /** A turn is running iff the session is active AND its
   *  duration isn't yet known. */
  function isRunningTurn(msg: Message): boolean {
    if (!sessionTiming.timing?.running) return false;
    const turn = turnByMessage.get(msg.id);
    return turn != null && turn.duration_ms == null;
  }

  let copyTimer: ReturnType<typeof setTimeout>;

  async function handleCopy() {
    const combined = messages.map((m) => formatMessageForCopy(m)).join("\n\n");
    const ok = await copyToClipboard(combined);
    if (ok) {
      clearTimeout(copyTimer);
      copied = true;
      copyTimer = setTimeout(() => { copied = false; }, 1500);
    }
  }
</script>

<div class="tool-group">
  <div class="tool-group-header">
    <span class="gear-icon">
      <svg width="12" height="12" viewBox="0 0 16 16"
        fill="var(--accent-amber)">
        <path d="M8 4.754a3.246 3.246 0 100 6.492
          3.246 3.246 0 000-6.492zM5.754 8a2.246
          2.246 0 114.492 0 2.246 2.246 0
          01-4.492 0z"/>
        <path d="M9.796 1.343c-.527-1.79-3.065-1.79-3.592
          0l-.094.319a.873.873 0
          01-1.255.52l-.292-.16c-1.64-.892-3.433.902-2.54
          2.541l.159.292a.873.873 0
          01-.52 1.255l-.319.094c-1.79.527-1.79 3.065
          0 3.592l.319.094a.873.873 0
          01.52 1.255l-.16.292c-.892 1.64.901 3.434
          2.541 2.54l.292-.159a.873.873 0
          011.255.52l.094.319c.527 1.79 3.065 1.79
          3.592 0l.094-.319a.873.873 0
          011.255-.52l.292.16c1.64.893 3.434-.902
          2.54-2.541l-.159-.292a.873.873 0
          01.52-1.255l.319-.094c1.79-.527 1.79-3.065
          0-3.592l-.319-.094a.873.873 0
          01-.52-1.255l.16-.292c.893-1.64-.902-3.433-2.541-2.54l-.292.159a.873.873
          0 01-1.255-.52l-.094-.319zm-2.633.283a.909.909
          0 011.674 0l.094.319a1.873 1.873 0
          002.693 1.115l.291-.16a.909.909 0
          011.18 1.18l-.159.292a1.873 1.873 0
          001.116 2.692l.318.094a.909.909 0
          010 1.674l-.319.094a1.873 1.873 0
          00-1.115 2.693l.16.291a.909.909 0
          01-1.18 1.18l-.292-.159a1.873 1.873 0
          00-2.692 1.116l-.094.318a.909.909 0
          01-1.674 0l-.094-.319a1.873 1.873 0
          00-2.693-1.115l-.291.16a.909.909 0
          01-1.18-1.18l.159-.292a1.873 1.873 0
          00-1.116-2.692l-.318-.094a.909.909 0
          010-1.674l.319-.094a1.873 1.873 0
          001.115-2.693l-.16-.291a.909.909 0
          011.18-1.18l.292.159a1.873 1.873 0
          002.692-1.116l.094-.318z"/>
      </svg>
    </span>
    <span class="group-label">{label}</span>
    <CopyButton
      {copied}
      ariaLabel="Copy tool calls"
      copiedAriaLabel="Copied tool calls"
      title="Copy tool calls"
      copiedTitle="Copied!"
      onclick={handleCopy}
    />
    <span class="group-timestamp">
      {formatTimestamp(timestamp)}
    </span>
  </div>

  <div class="tool-group-body">
    {#each messages as message (message.id)}
      {@const calls = message.tool_calls ?? []}
      {@const turn = turnByMessage.get(message.id)}
      {#if calls.length === 1}
        {@const soloCall = calls[0]!}
        <ToolBlock
          toolCall={soloCall}
          content=""
          label={displayToolName(soloCall)}
          durationLabel={soloDurationLabel(
            callByToolUseID.get(soloCall.tool_use_id ?? ""),
            turn,
            message,
          )}
          isRunning={isRunningTurn(message)}
          {highlightQuery}
          {isCurrentHighlight}
        />
      {:else if calls.length >= 2}
        <ParallelGroup
          toolCalls={calls}
          callTimingByID={callByToolUseID}
          turnDurationMs={turn?.duration_ms ?? null}
          isRunning={isRunningTurn(message)}
          {highlightQuery}
          {isCurrentHighlight}
        />
      {:else}
        <!-- Fallback for messages with `has_tool_use` but no
             structured tool_calls — parse the content for tool
             markers (legacy/synthetic transcripts). -->
        {#each enrichSegments(parseContent(message.content, message.has_tool_use, message.id, message.content_length), message.tool_calls).filter((s) => s.type === "tool") as seg, segIdx (`${message.id}-${segIdx}`)}
          <ToolBlock
            content={seg.content}
            label={seg.label}
            toolCall={seg.toolCall}
            {highlightQuery}
            {isCurrentHighlight}
          />
        {/each}
      {/if}
    {/each}
  </div>
</div>

<style>
  .tool-group {
    border-left: 3px solid var(--accent-amber);
    background: var(--tool-bg);
    border-radius: 0 var(--radius-md) var(--radius-md) 0;
    padding: 8px 12px;
  }

  .tool-group-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
  }

  .gear-icon {
    display: flex;
    align-items: center;
    flex-shrink: 0;
  }

  .group-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--accent-amber);
  }

  .group-timestamp {
    font-size: 12px;
    color: var(--text-muted);
    margin-left: auto;
  }

  .tool-group:hover :global(.copy-btn) {
    opacity: 1;
  }

  .tool-group-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .tool-group-body :global(.tool-block) {
    margin: 0;
    border-left: none;
    border-radius: 0;
  }
</style>
