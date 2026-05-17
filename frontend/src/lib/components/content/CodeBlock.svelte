<script lang="ts">
  import { onDestroy } from "svelte";
  import { copyToClipboard } from "../../utils/clipboard.js";
  import { applyHighlight, escapeHTML } from "../../utils/highlight.js";

  interface Props {
    content: string;
    language?: string;
    highlightQuery?: string;
    isCurrentHighlight?: boolean;
  }

  let { content, language, highlightQuery = "", isCurrentHighlight = false }: Props = $props();
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  async function handleCopy() {
    const ok = await copyToClipboard(content);
    if (!ok) return;

    clearTimeout(copyTimer);
    copied = true;
    copyTimer = setTimeout(() => {
      copied = false;
    }, 1500);
  }

  onDestroy(() => {
    clearTimeout(copyTimer);
  });
</script>

<div class="code-block">
  <button
    class="copy-code"
    type="button"
    aria-label={copied ? "Copied code block" : "Copy code block"}
    title={copied ? "Copied" : "Copy code"}
    onclick={handleCopy}
  >
    {copied ? "Copied" : "Copy"}
  </button>
  {#if language}
    <div class="code-lang">{language}</div>
  {/if}
  <pre
    class="code-content"
    use:applyHighlight={{ q: highlightQuery, current: isCurrentHighlight, content }}
  ><code>{@html escapeHTML(content)}</code></pre>
</div>

<style>
  .code-block {
    position: relative;
    background: var(--code-bg);
    border-radius: var(--radius-md);
    margin: 4px 0;
    overflow: hidden;
  }

  .copy-code {
    position: absolute;
    top: 6px;
    right: 6px;
    z-index: 1;
    padding: 3px 8px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: var(--radius-sm);
    background: rgba(15, 23, 42, 0.88);
    color: var(--code-text);
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.4;
    opacity: 0;
    transform: translateY(-2px);
    transition:
      opacity 120ms ease,
      transform 120ms ease,
      border-color 120ms ease;
  }

  .copy-code:hover,
  .copy-code:focus-visible,
  .code-block:hover .copy-code,
  .code-block:focus-within .copy-code {
    opacity: 1;
    transform: translateY(0);
  }

  .copy-code:hover,
  .copy-code:focus-visible {
    border-color: rgba(255, 255, 255, 0.28);
  }

  .code-lang {
    padding: 4px 12px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 500;
    color: var(--code-text);
    opacity: 0.5;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .code-content {
    padding: 12px 16px;
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.55;
    color: var(--code-text);
    overflow-x: auto;
  }

  .code-content code {
    font-family: inherit;
  }

  @media (max-width: 767px) {
    .code-content {
      max-width: calc(100vw - 32px);
    }
  }
</style>
