// "Ordner archivieren": preview → confirm → result (zeitgestempelt / ausstehend).

import { api, errMsg, list, type Deposit, type Preview } from "../api";
import { clear, h, icon } from "../dom";
import { bytes, date, dateTimeSeconds, n, plural, seq } from "../fmt";
import { app, navigate } from "../state";
import { PROBLEM_TEXT, QUAL, REASON_RESULT, REASON_SHORT, STATE } from "../text";
import { button, openDialog, pathText, toast, type DialogHandle } from "../ui";
import { refreshStatus, withLock } from "../actions";

let openNow = false;

export async function openArchiveDialog(folder: string): Promise<void> {
  if (openNow) return;
  let preview: Preview;
  try {
    preview = await api.Preview(folder);
  } catch (e) {
    toast(errMsg(e), "error");
    return;
  }
  openNow = true;
  const dlg = openDialog("dlg-title", "narrow", () => {
    openNow = false;
    void refreshStatus();
  });
  confirmStep(dlg, preview);
}

function confirmStep(dlg: DialogHandle, p: Preview): void {
  const years = Array.from(new Set([p.suggestedYear, ...list(p.years).map((y) => y.year)])).sort((a, b) => b - a);
  const problems = list(p.problems);
  let retentionYears = p.retentionDefault;

  const name = h("input", { id: "ablage-name", type: "text", class: "input", value: p.suggestedName, required: true });
  const year = h("select", { id: "belegjahr", class: "input input--select" }, ...years.map((y) => h("option", { value: String(y), selected: y === p.suggestedYear }, String(y))));

  const retentionText = h("span", { class: "strong-text" });
  const retentionSelect = h(
    "select",
    { id: "aufbewahrung", class: "input input--select input--inline", "aria-label": "Aufbewahrung" },
    ...list(p.retentionOptions).map((o) => h("option", { value: String(o.years), selected: o.years === retentionYears }, o.label)),
  );
  const retentionEdit = h("div", { class: "retention-edit", hidden: true }, h("label", { for: "aufbewahrung", class: "sr-only" }, "Aufbewahrung"), retentionSelect);
  const updateRetention = async () => {
    try {
      const until = await api.RetentionUntil(Number(year.value), retentionYears);
      retentionText.textContent = `${retentionYears} Jahre, bis ${date(until)}`;
    } catch (e) {
      retentionText.textContent = `${retentionYears} Jahre`;
      toast(errMsg(e), "error");
    }
  };
  year.addEventListener("change", () => {
    void updateRetention();
    renderHints();
  });
  retentionSelect.addEventListener("change", () => {
    retentionYears = Number(retentionSelect.value);
    void updateRetention();
  });
  void updateRetention();
  const changeBtn = button("ändern", () => {
    retentionEdit.hidden = false;
    changeBtn.hidden = true;
    retentionSelect.focus();
  }, "plain-link", { class: "btn--target", "aria-label": "Aufbewahrung ändern" });

  const hints = h("ul", { class: "hints" });
  const renderHints = () => {
    clear(hints);
    for (const dup of list(p.duplicates)) {
      hints.appendChild(
        h(
          "li",
          null,
          dup.files === 1
            ? `1 Datei ist identisch mit einer Datei ${where(dup.seq)}. Prüfen Sie, ob sie doppelt abgelegt ist.`
            : `${n(dup.files)} Dateien sind identisch mit Dateien ${where(dup.seq)}. Prüfen Sie, ob sie doppelt abgelegt sind.`,
        ),
      );
    }
    const skipped = list(p.skipped);
    if (skipped.length) {
      hints.appendChild(
        h(
          "li",
          null,
          skipped.length === 1
            ? `1 Systemdatei wird übersprungen (${skipped[0]}).`
            : `${n(skipped.length)} Systemdateien werden übersprungen (${skipped.join(", ")}).`,
        ),
      );
    }
    const other = list(p.years).map((y) => y.year).filter((y) => y !== Number(year.value)).sort();
    if (other.length) {
      const yrs = other.length === 1 ? String(other[0]) : `${other.slice(0, -1).join(", ")} und ${other[other.length - 1]}`;
      hints.appendChild(h("li", null, `Einige Dateien stammen aus ${yrs}. Das Belegjahr gilt für die ganze Ablage; Belege anderer Jahre legen Sie bei Bedarf in einen eigenen Ordner.`));
    }
    hints.hidden = hints.childElementCount === 0;
  };
  renderHints();

  const blocked = problems.length > 0 || p.files === 0;
  const archiveBtn = button("Archivieren", () => void run(), "primary", { disabled: blocked });
  const cancelBtn = button("Abbrechen", () => dlg.close());

  const run = async () => {
    if (!name.value.trim()) {
      toast("Bitte geben Sie der Ablage einen Namen.", "error");
      name.focus();
      return;
    }
    dlg.lock(true);
    archiveBtn.disabled = true;
    cancelBtn.disabled = true;
    archiveBtn.textContent = "Wird archiviert …";
    try {
      const r = await withLock(() => api.Archive({ folder: p.folder, name: name.value.trim(), belegjahr: Number(year.value), retentionYears }));
      dlg.lock(false);
      resultStep(dlg, r.deposit, r.warning);
    } catch (e) {
      dlg.lock(false);
      archiveBtn.disabled = false;
      cancelBtn.disabled = false;
      archiveBtn.textContent = "Archivieren";
      toast(errMsg(e), "error");
    }
  };

  clear(dlg.body);
  dlg.body.append(
    h(
      "div",
      { class: "dialog__content" },
      h("div", null, h("h1", { id: "dlg-title", class: "dialog__title" }, "Ordner archivieren"), h("div", { class: "mono mono--small muted mt4 break" }, pathText(p.folder))),
      h(
        "div",
        { class: "mini-tiles" },
        h("div", { class: "mini-tile mini-tile--big" }, h("div", { class: "mini-tile__label" }, "Dateien"), h("div", { class: "mini-tile__value" }, n(p.files))),
        h("div", { class: "mini-tile mini-tile--big" }, h("div", { class: "mini-tile__label" }, "Größe"), h("div", { class: "mini-tile__value" }, bytes(p.bytes))),
      ),
      p.files === 0 ? h("p", { class: "problem-box", role: "alert" }, "Der Ordner enthält keine Dateien, die archiviert werden können.") : null,
      problems.length
        ? h(
            "div",
            { class: "problem-box", role: "alert" },
            h("strong", null, `${plural(problems.length, "Datei kann", "Dateien können")} nicht archiviert werden:`),
            h("ul", null, ...problems.map((x) => h("li", null, h("span", { class: "mono mono--small" }, x.path), ` – ${PROBLEM_TEXT[x.kind] ?? x.kind}`))),
            h("div", null, "Benennen Sie die Dateien um oder speichern Sie sie auf diesem PC und wählen Sie den Ordner dann erneut."),
          )
        : null,
      hints,
      h(
        "div",
        { class: "field-row" },
        h("div", { class: "field field--name" }, h("label", { for: "ablage-name", class: "field__label" }, "Name der Ablage"), name),
        h("div", { class: "field field--year" }, h("label", { for: "belegjahr", class: "field__label" }, "Belegjahr"), year),
      ),
      h("div", { class: "retention" }, h("span", null, "Aufbewahrung: ", retentionText), changeBtn, retentionEdit),
    ),
    h("div", { class: "dialog__actions" }, cancelBtn, archiveBtn),
  );
  (blocked ? cancelBtn : name).focus();
}

/** "in Ablage 0011", or "in früheren Ablagen" when the core does not say which. */
function where(s: number): string {
  return s > 0 ? `in Ablage ${seq(s)}` : "in früheren Ablagen";
}

function resultStep(dlg: DialogHandle, d: Deposit, warning = ""): void {
  const done = button("Fertig", () => dlg.close(), "primary");
  const sub = `Ablage ${seq(d.seq)} · ${d.name} · ${plural(d.files, "Datei", "Dateien")}`;
  const warn = warning ? h("p", { class: "problem-box", role: "alert" }, warning) : null;
  clear(dlg.body);

  if (d.state === STATE.stamped) {
    const qual = d.qualification === QUAL.qualified ? "Qualifizierter Zeitstempel" : "Zeitstempel";
    dlg.body.append(
      h(
        "div",
        { class: "dialog__content dialog__content--done" },
        h(
          "div",
          { class: "done-head" },
          h("span", { class: "done-icon done-icon--green" }, icon("check", 24, 2.4)),
          h("div", null, h("h1", { id: "dlg-title", class: "dialog__title" }, "Archiviert und zeitgestempelt"), h("div", { class: "muted small" }, sub)),
        ),
        h(
          "div",
          { class: "stamp-box" },
          h("span", { class: "state--green" }, icon("check", 16, 2.4)),
          h("span", { class: "grow" }, `${qual} · ${d.provider}`),
          h("span", { class: "muted small" }, dateTimeSeconds(d.genTime)),
        ),
        warn,
        h("p", { class: "muted small m0" }, "Tipp: Jetzt ist ein guter Zeitpunkt für die Sicherung des Archivordners."),
      ),
      h("div", { class: "dialog__actions dialog__actions--done" }, done),
    );
    done.focus();
    return;
  }

  const t = REASON_RESULT[d.reason] ?? REASON_RESULT.anbieter;
  let action: HTMLElement;
  if (d.reason === "kein-dienst") {
    action = button("Jetzt einrichten", () => {
      dlg.close();
      navigate("#/einstellungen");
    }, "amber");
  } else {
    const retry: HTMLButtonElement = button("Jetzt nachholen", async () => {
      retry.disabled = true;
      retry.textContent = "Wird geholt …";
      try {
        const r = await withLock(() => api.StampPending());
        const st = await api.Status();
        app.status = st;
        const now = list(st.deposits).find((x) => x.seq === d.seq);
        if (now && now.state === STATE.stamped) {
          resultStep(dlg, now);
          return;
        }
        toast([`Zeitstempel weiterhin ausstehend${r.reason ? ` (${REASON_SHORT[r.reason] ?? r.reason})` : ""}.`, ...list(r.warnings)].join(" "), "error");
      } catch (e) {
        toast(errMsg(e), "error");
      }
      retry.disabled = false;
      retry.textContent = "Jetzt nachholen";
    }, "amber");
    action = retry;
  }
  dlg.body.append(
    h(
      "div",
      { class: "dialog__content dialog__content--done" },
      h(
        "div",
        { class: "done-head" },
        h("span", { class: "done-icon done-icon--amber" }, icon("clock", 24, 2.2)),
        h("div", null, h("h1", { id: "dlg-title", class: "dialog__title" }, "Archiviert – Zeitstempel ausstehend"), h("div", { class: "muted small" }, sub)),
      ),
      h(
        "div",
        { class: "pending-box", role: "status" },
        h("div", { class: "pending-box__title" }, t.title),
        h("div", { class: "pending-box__text" }, t.text),
        h("div", { class: "mt10" }, action),
      ),
      warn,
    ),
    h("div", { class: "dialog__actions dialog__actions--done" }, done),
  );
  done.focus();
}
