// Renders the small Markdown subset of docs/haftungsausschluss.md
// (headings, paragraphs, "- " lists, [text](link)) as DOM nodes.
// Links are shown as plain text: the app does not navigate anywhere.

import { h } from "./dom";

function inline(text: string): string {
  return text.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1").replace(/\*\*([^*]+)\*\*/g, "$1");
}

export function renderMarkdown(md: string, headingOffset = 1): HTMLElement {
  const root = h("div", { class: "prose" });
  const blocks = md.replace(/\r\n/g, "\n").split(/\n{2,}/);
  for (const raw of blocks) {
    const block = raw.trim();
    if (!block) continue;
    const m = block.match(/^(#{1,6})\s+(.*)$/);
    if (m && !block.includes("\n")) {
      const level = Math.min(6, m[1].length + headingOffset);
      root.appendChild(h(`h${level}` as "h2", null, inline(m[2])));
      continue;
    }
    const lines = block.split("\n");
    if (lines.every((l) => /^\s*[-*]\s+/.test(l))) {
      root.appendChild(h("ul", null, ...lines.map((l) => h("li", null, inline(l.replace(/^\s*[-*]\s+/, ""))))));
      continue;
    }
    root.appendChild(h("p", null, inline(lines.join(" "))));
  }
  return root;
}
