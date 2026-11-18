// Formatting for German UI text. Times are shown in Austrian local time.

const TZ = "Europe/Vienna";
const num = new Intl.NumberFormat("de-DE");
const dec1 = new Intl.NumberFormat("de-DE", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const dateFmt = new Intl.DateTimeFormat("de-DE", { timeZone: TZ, day: "2-digit", month: "2-digit", year: "numeric" });
const hmFmt = new Intl.DateTimeFormat("de-DE", { timeZone: TZ, hour: "2-digit", minute: "2-digit" });
const hmsFmt = new Intl.DateTimeFormat("de-DE", { timeZone: TZ, hour: "2-digit", minute: "2-digit", second: "2-digit" });
const tzFmt = new Intl.DateTimeFormat("de-DE", { timeZone: TZ, timeZoneName: "short" });

export const n = (v: number): string => num.format(v);
export const seq = (s: number): string => String(s).padStart(4, "0");

/** 1 Datei / 2 Dateien */
export function plural(count: number, one: string, many: string): string {
  return `${n(count)} ${count === 1 ? one : many}`;
}

export function bytes(b: number): string {
  const mb = b / (1024 * 1024);
  if (mb >= 1024) return `${dec1.format(mb / 1024)} GB`;
  if (mb >= 0.1) return `${dec1.format(mb)} MB`;
  return `${n(Math.max(1, Math.round(b / 1024)))} KB`;
}

function parse(iso: string): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return isNaN(d.getTime()) ? null : d;
}

function tzName(d: Date): string {
  const part = tzFmt.formatToParts(d).find((p) => p.type === "timeZoneName");
  return part ? part.value : "";
}

/** 05.11.2026 */
export function date(iso: string): string {
  const d = parse(iso.length === 10 ? iso + "T12:00:00Z" : iso);
  return d ? dateFmt.format(d) : "";
}

/** 05.11.2026, 09:14 */
export function dateTimeShort(iso: string): string {
  const d = parse(iso);
  return d ? `${dateFmt.format(d)}, ${hmFmt.format(d)}` : "";
}

/** 18.11.2026, 10:32:04 */
export function dateTimeSeconds(iso: string): string {
  const d = parse(iso);
  return d ? `${dateFmt.format(d)}, ${hmsFmt.format(d)}` : "";
}

/** 05.11.2026, 09:14:23 Uhr MEZ */
export function dateTimeLong(iso: string): string {
  const d = parse(iso);
  return d ? `${dateFmt.format(d)}, ${hmsFmt.format(d)} Uhr ${tzName(d)}` : "";
}

function sameDay(a: Date, b: Date): boolean {
  return dateFmt.format(a) === dateFmt.format(b);
}

/** "heute, 10:32 Uhr" or "05.11.2026, 10:32 Uhr" */
export function whenRelative(iso: string, capital = false): string {
  const d = parse(iso);
  if (!d) return "";
  if (sameDay(d, new Date())) return `${capital ? "Heute" : "heute"}, ${hmFmt.format(d)} Uhr`;
  return `${dateFmt.format(d)}, ${hmFmt.format(d)} Uhr`;
}

export function accuracy(ms: number): string {
  if (ms <= 0) return "Genauigkeit nicht angegeben";
  if (ms % 1000 === 0) return `± ${n(ms / 1000)} s`;
  return `± ${new Intl.NumberFormat("de-DE", { maximumFractionDigits: 3 }).format(ms / 1000)} s`;
}

const COUNTRIES: Record<string, string> = {
  AT: "Österreich", BE: "Belgien", BG: "Bulgarien", CY: "Zypern", CZ: "Tschechien", DE: "Deutschland",
  DK: "Dänemark", EE: "Estland", ES: "Spanien", FI: "Finnland", FR: "Frankreich", GR: "Griechenland",
  HR: "Kroatien", HU: "Ungarn", IE: "Irland", IT: "Italien", LT: "Litauen", LU: "Luxemburg",
  LV: "Lettland", MT: "Malta", NL: "Niederlande", PL: "Polen", PT: "Portugal", RO: "Rumänien",
  SE: "Schweden", SI: "Slowenien", SK: "Slowakei",
};

export const country = (cc: string): string => COUNTRIES[cc] ?? cc;
