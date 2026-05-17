# Code Block Copy Design

## Context

Session messages are rendered by `MessageContent.svelte`. Fenced code blocks
parsed from message content become `code` segments and render through
`CodeBlock.svelte`. Markdown prose still renders through sanitized HTML from
`renderMarkdown()`, so interactive copy controls should not be injected through
`{@html}`.

## Goal

Add a hover/focus copy affordance to every session code block. The button copies
the exact raw code block content, not the rendered HTML, language label, or
surrounding message text.

## Approach

Implement the feature in `CodeBlock.svelte`, the component that already owns
session code block presentation. This covers markdown fenced blocks and any
other session code segments that use the same component, while keeping sanitizer
behavior unchanged.

## Behavior

- Render a small copy button in the top-right corner of each code block.
- Show the button when the code block is hovered or when the button receives
  keyboard focus.
- Copy the `content` prop exactly via the existing `copyToClipboard()` utility.
- Show brief success feedback by changing the button label/title to `Copied`
  after a successful copy.
- Keep the button accessible with a descriptive `aria-label` and native button
  keyboard behavior.

## Testing

Add component coverage through the existing Svelte test setup. Tests should
verify that a message containing a fenced markdown block renders a copy button,
clicking it copies the exact code content, and successful copy feedback appears.

## Non-Goals

- No changes to markdown sanitization.
- No copy affordance for arbitrary inline code spans.
- No copy controls injected into `renderMarkdown()` HTML.
