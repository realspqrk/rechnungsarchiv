// Minimal DOM helpers. Text always goes through text nodes, never innerHTML,
// so file names and other data cannot inject markup.

type Child = Node | string | number | null | undefined | false | Child[];
type Attrs = Record<string, unknown>;

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Attrs | null = null,
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) {
      if (value === undefined || value === null || value === false) continue;
      if (key.startsWith("on") && typeof value === "function") {
        el.addEventListener(key.slice(2).toLowerCase(), value as EventListener);
      } else if (key === "class") {
        el.className = String(value);
      } else if (key === "value" && "value" in el) {
        (el as HTMLInputElement).value = String(value);
      } else if (key === "checked" || key === "disabled" || key === "selected" || key === "open") {
        (el as unknown as Record<string, boolean>)[key] = Boolean(value);
      } else {
        el.setAttribute(key, value === true ? "" : String(value));
      }
    }
  }
  append(el, children);
  return el;
}

export function append(parent: Node, children: Child[]): void {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    if (Array.isArray(c)) append(parent, c);
    else if (c instanceof Node) parent.appendChild(c);
    else parent.appendChild(document.createTextNode(String(c)));
  }
}

export function clear(el: Element): void {
  while (el.firstChild) el.removeChild(el.firstChild);
}

// Icons: fixed path data (constants, not data from the core).
const ICONS: Record<string, string[]> = {
  logo: ["rect:3,4,18,5,1", "M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9", "M10 13h4"],
  check: ["M5 12l4 4 10-10"],
  cross: ["M6 6l12 12M18 6L6 18"],
  clock: ["circle:12,12,9", "M12 7v5l3 2"],
  info: ["circle:12,12,9", "M12 11v5M12 8h.01"],
  warn: ["M12 3l9 16H3z", "M12 10v4M12 17h.01"],
  search: ["circle:11,11,7", "M20 20l-4-4"],
  folderUp: ["M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z", "M12 10v6", "M9 13l3-3 3 3"],
  gear: ["circle:12,12,3", "M12 2v3M12 19v3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M2 12h3M19 12h3M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1"],
  back: ["M15 6l-6 6 6 6"],
  lock: ["rect:5,11,14,9,2", "M8 11V8a4 4 0 0 1 8 0v3"],
};

const SVG = "http://www.w3.org/2000/svg";

export function icon(name: keyof typeof ICONS | string, size = 20, strokeWidth = 2, cls = ""): SVGSVGElement {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", String(strokeWidth));
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  if (cls) svg.setAttribute("class", cls);
  for (const part of ICONS[name] ?? []) {
    if (part.startsWith("rect:")) {
      const [x, y, w, hh, r] = part.slice(5).split(",");
      const el = document.createElementNS(SVG, "rect");
      el.setAttribute("x", x);
      el.setAttribute("y", y);
      el.setAttribute("width", w);
      el.setAttribute("height", hh);
      el.setAttribute("rx", r);
      svg.appendChild(el);
    } else if (part.startsWith("circle:")) {
      const [cx, cy, r] = part.slice(7).split(",");
      const el = document.createElementNS(SVG, "circle");
      el.setAttribute("cx", cx);
      el.setAttribute("cy", cy);
      el.setAttribute("r", r);
      svg.appendChild(el);
    } else {
      const el = document.createElementNS(SVG, "path");
      el.setAttribute("d", part);
      svg.appendChild(el);
    }
  }
  return svg;
}
