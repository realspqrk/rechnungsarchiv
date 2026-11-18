// "Über" and "Haftungsausschluss". The disclaimer text comes only from the
// embedded docs/haftungsausschluss.md (via Go), never from frontend copies.

import { api, errMsg } from "../api";
import { h } from "../dom";
import { renderMarkdown } from "../markdown";
import { app } from "../state";
import { button, openDialog, scrollCue, toast } from "../ui";

function splitTitle(md: string): { title: string; rest: string } {
  const m = md.replace(/\r\n/g, "\n").match(/^#\s+(.+)\n/);
  return m ? { title: m[1].trim(), rest: md.slice(m[0].length) } : { title: "Haftungsausschluss", rest: md };
}

export function openDisclaimerDialog(): void {
  const { title, rest } = splitTitle(app.disclaimer?.markdown ?? "");
  const dlg = openDialog("hinweis-title", "wide");
  const close = button("Schließen", () => dlg.close(), "primary");
  const content = h("div", { class: "dialog__content", "aria-labelledby": "hinweis-title" }, h("h1", { id: "hinweis-title", class: "dialog__title" }, title), renderMarkdown(rest, 0));
  const actions = h("div", { class: "dialog__actions" }, close);
  dlg.body.append(content, actions);
  scrollCue(content, actions);
  close.focus({ preventScroll: true });
}

export function openAboutDialog(): void {
  const about = app.about;
  const { title, rest } = splitTitle(app.disclaimer?.markdown ?? "");
  const dlg = openDialog("ueber-title", "wide");
  const close = button("Schließen", () => dlg.close(), "primary");
  const project = button(["Projektseite ", h("span", { "aria-hidden": "true" }, "↗")], async () => {
    try {
      await api.OpenProject();
    } catch (e) {
      toast(errMsg(e), "error");
    }
  }, "link", { "aria-label": "Projektseite (öffnet den Browser)" });

  const content = h(
      "div",
      { class: "dialog__content", "aria-labelledby": "ueber-title" },
      h("h1", { id: "ueber-title", class: "dialog__title" }, "Über Rechnungsarchiv"),
      h(
        "dl",
        { class: "facts" },
        h("dt", null, "Version"),
        h("dd", { class: "mono" }, about?.version ?? ""),
        h("dt", null, "Lizenz"),
        h("dd", null, `${about?.license ?? ""} (freie Open-Source-Software; Lizenztext in der Datei LICENSE)`),
        h("dt", null, "Schrift"),
        h("dd", null, "IBM Plex Sans und IBM Plex Mono (SIL Open Font License 1.1)"),
      ),
      h("div", null, project),
      h(
        "p",
        { class: "muted small m0" },
        "Danke an die offenen Standards und Projekte, auf denen das hier aufbaut: BagIt (Library of Congress, RFC 8493), RFC 3161, die EU-Vertrauenslisten der Europäischen Kommission und der Mitgliedstaaten, digitorus/timestamp (Go), Wails und GoBDify für die Anregung.",
      ),
      // The Kurzfassung paragraph is shown unchanged here (docs/haftungsausschluss.md).
      h("div", { class: "notice", role: "note" }, h("p", null, app.disclaimer?.short ?? "")),
      h("h2", { class: "card__h card__h--tight" }, title),
      renderMarkdown(rest, 1),
    );
  const actions = h("div", { class: "dialog__actions" }, close);
  dlg.body.append(content, actions);
  scrollCue(content, actions);
  close.focus({ preventScroll: true });
}
