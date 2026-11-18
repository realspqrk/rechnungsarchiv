// "Export für Betriebsprüfung": Belegjahre, Ziel, content summary, result.

import { api, errMsg, list, type ExportPlan } from "../api";
import { clear, h } from "../dom";
import { bytes, plural } from "../fmt";
import { button, openDialog, toast } from "../ui";

export async function openExportDialog(): Promise<void> {
  let plan: ExportPlan;
  try {
    plan = await api.ExportPlan(0, 0);
  } catch (e) {
    toast(errMsg(e), "error");
    return;
  }
  const years = list(plan.years);
  const dlg = openDialog("exp-title", "narrow");
  let target = plan.suggestedTarget;
  let pickedByUser = false;

  const yearSelect = (id: string, value: number) =>
    h("select", { id, class: "input input--select" }, ...years.map((y) => h("option", { value: String(y), selected: y === value }, String(y))));
  const from = yearSelect("von", plan.from);
  const to = yearSelect("bis", plan.to);
  const targetBox = h("span", { class: "path-box mono", id: "ziel-wert" }, target || "Noch nicht gewählt");
  const summary = h("div", { class: "summary__title" });
  const exportBtn = button("Exportieren", () => void run(), "primary");
  const cancelBtn = button("Abbrechen", () => dlg.close());
  const doneBox = h("div", { class: "export-done", hidden: true, role: "status" });
  const actions = h("div", { class: "dialog__actions" }, cancelBtn, exportBtn);

  const update = async () => {
    try {
      plan = await api.ExportPlan(Number(from.value), Number(to.value));
      if (!pickedByUser) {
        target = plan.suggestedTarget;
        targetBox.textContent = target;
      }
      summary.textContent =
        plan.deposits === 0
          ? "Inhalt: keine Ablage in diesen Belegjahren"
          : `Inhalt: ${plural(plan.deposits, "Ablage", "Ablagen")} · ${plural(plan.files, "Datei", "Dateien")} · ${bytes(plan.bytes)}`;
      exportBtn.disabled = plan.deposits === 0 || !target;
    } catch (e) {
      toast(errMsg(e), "error");
    }
  };
  from.addEventListener("change", () => void update());
  to.addEventListener("change", () => void update());

  const change = button("Ändern …", async () => {
    try {
      const picked = await api.PickFolder("export");
      if (picked) {
        target = picked;
        pickedByUser = true;
        targetBox.textContent = picked;
        exportBtn.disabled = plan.deposits === 0;
      }
    } catch (e) {
      toast(errMsg(e), "error");
    }
  }, "secondary", { "aria-describedby": "ziel-label" });

  const run = async () => {
    dlg.lock(true);
    exportBtn.disabled = true;
    cancelBtn.disabled = true;
    from.disabled = true;
    to.disabled = true;
    change.disabled = true;
    exportBtn.textContent = "Wird exportiert …";
    try {
      const r = await api.Export({ from: Number(from.value), to: Number(to.value), target });
      dlg.lock(false);
      clear(doneBox);
      doneBox.append(
        h("span", { class: "grow" }, h("strong", null, "Export abgeschlossen."), ` ${plural(r.deposits, "Ablage", "Ablagen")} in ${r.target}`),
        button("Ordner öffnen", async () => {
          try {
            await api.OpenFolder(r.target);
          } catch (e) {
            toast(errMsg(e), "error");
          }
        }, "secondary", { class: "btn--on-green" }),
      );
      for (const w of list(r.warnings)) doneBox.append(h("p", { class: "export-done__warn" }, w));
      doneBox.hidden = false;
      clear(actions);
      const done = button("Fertig", () => dlg.close(), "primary");
      actions.append(done);
      done.focus();
    } catch (e) {
      dlg.lock(false);
      exportBtn.textContent = "Exportieren";
      exportBtn.disabled = false;
      cancelBtn.disabled = false;
      from.disabled = false;
      to.disabled = false;
      change.disabled = false;
      toast(errMsg(e), "error");
    }
  };

  const mono = (t: string) => h("span", { class: "mono mono--small" }, t);
  dlg.body.append(
    h(
      "div",
      { class: "dialog__content" },
      h("h1", { id: "exp-title", class: "dialog__title" }, "Export für Betriebsprüfung"),
      h(
        "div",
        { class: "field-row" },
        h("div", { class: "field field--half" }, h("label", { for: "von", class: "field__label" }, "Belegjahr von"), from),
        h("div", { class: "field field--half" }, h("label", { for: "bis", class: "field__label" }, "bis"), to),
      ),
      h("div", { class: "field" }, h("span", { class: "field__label", id: "ziel-label" }, "Ziel"), h("div", { class: "path-row" }, targetBox, change)),
      h(
        "div",
        { class: "summary" },
        summary,
        h(
          "ul",
          { class: "hints" },
          h("li", null, "je Ablage ein ", h("strong", { class: "medium" }, "Archivprotokoll (PDF)"), " – Feststellung mit Zeitstempel und vollständigem Dateiverzeichnis"),
          h("li", null, mono("DATEIVERZEICHNIS.csv"), " – alle Dateien mit Ablage, Belegjahr und Zeitstempel"),
          h("li", null, "Ablagen, Zeitstempel und EU-Vertrauenslisten, unverändert"),
          h("li", null, mono("rechnungsarchiv.exe"), " und ", mono("PRUEFANLEITUNG.txt"), " – zum selbst Kontrollieren"),
        ),
      ),
    ),
    doneBox,
    actions,
  );
  await update();
  from.focus();
}
