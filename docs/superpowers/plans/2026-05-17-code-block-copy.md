# Code Block Copy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a hover/focus copy button to every session code block.

**Architecture:** Keep the behavior in `CodeBlock.svelte`, which already owns
session code block rendering. The component will copy its raw `content` prop
with the existing clipboard utility and expose a keyboard-accessible button with
brief success feedback.

**Tech Stack:** Svelte 5, TypeScript, Vitest with jsdom, existing
`copyToClipboard()` utility.

______________________________________________________________________

## File Structure

- Modify `frontend/src/lib/components/content/MessageContent.test.ts`: add a
  focused test that renders a fenced markdown block, clicks its code-block copy
  button, and verifies exact copied text and success feedback.
- Modify `frontend/src/lib/components/content/CodeBlock.svelte`: import
  `copyToClipboard()`, render the copy button, manage copied-state timeout
  cleanup, and add hover/focus styling.

## Task 1: Add Copy Button Behavior

**Files:**

- Test: `frontend/src/lib/components/content/MessageContent.test.ts`

- Modify: `frontend/src/lib/components/content/CodeBlock.svelte`

- [ ] **Step 1: Write the failing test**

In `frontend/src/lib/components/content/MessageContent.test.ts`, replace the
clipboard mock block with a hoisted mock so the new test can assert calls:

```ts
const copyToClipboardMock = vi.hoisted(() =>
  vi.fn().mockResolvedValue(true),
);

vi.mock("../../utils/clipboard.js", () => ({
  copyToClipboard: copyToClipboardMock,
}));
```

Update `afterEach` so mock calls do not leak between tests:

```ts
afterEach(() => {
  document.body.innerHTML = "";
  vi.clearAllMocks();
});
```

Add this test inside `describe("MessageContent", () => { ... })`:

```ts
it("copies the exact raw content from a fenced code block", async () => {
  const code = "const answer = 42;\n";
  const content = `Here is code:\n\n\`\`\`ts\n${code}\`\`\``;
  const component = mount(MessageContent, {
    target: document.body,
    props: {
      message: makeMessage({
        content,
        content_length: content.length,
      }),
    },
  });

  await tick();
  const copyButton = document.querySelector<HTMLButtonElement>(
    'button[aria-label="Copy code block"]',
  );
  expect(copyButton).not.toBeNull();

  copyButton!.click();
  await Promise.resolve();
  await tick();

  expect(copyToClipboardMock).toHaveBeenCalledWith(code);
  expect(copyButton!.getAttribute("aria-label")).toBe(
    "Copied code block",
  );
  expect(copyButton!.textContent?.trim()).toBe("Copied");

  unmount(component);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
npm test -- MessageContent.test.ts
```

Expected result: FAIL because no `button[aria-label="Copy code block"]` exists
yet.

- [ ] **Step 3: Implement minimal component behavior**

In `frontend/src/lib/components/content/CodeBlock.svelte`, update the script
block to:

```svelte
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
```

Update the markup to render the button before the language label:

```svelte
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
```

Add these styles inside the existing `<style>` block:

```css
.code-block {
  position: relative;
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
```

Keep the existing `.code-block` background, border radius, margin, and overflow
declarations in the same rule; add `position: relative` to that existing rule
instead of creating a duplicate rule if desired.

- [ ] **Step 4: Run the focused test to verify it passes**

Run:

```bash
npm test -- MessageContent.test.ts
```

Expected result: PASS for `MessageContent.test.ts`.

- [ ] **Step 5: Run frontend validation**

Run:

```bash
npm test -- MessageContent.test.ts && npm run check
```

Expected result: Vitest passes and `svelte-check` reports 0 errors.

- [ ] **Step 6: Commit implementation**

Run:

```bash
git status --short
git diff -- frontend/src/lib/components/content/CodeBlock.svelte frontend/src/lib/components/content/MessageContent.test.ts
git add frontend/src/lib/components/content/CodeBlock.svelte frontend/src/lib/components/content/MessageContent.test.ts
git commit -m "Add copy action to session code blocks"
```

Expected result: a focused implementation commit containing only the component
and test changes.
