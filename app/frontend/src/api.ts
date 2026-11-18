// The generated Wails bindings are the only door to Go. Re-exported here so
// views import one module.
export { Shell as api } from "../bindings/github.com/realspqrk/rechnungsarchiv/app";
export type * from "../bindings/github.com/realspqrk/rechnungsarchiv/app";
export { OrdnerWartet, StatusGeaendert } from "../bindings/events";
export { Events } from "@wailsio/runtime";

/** German message of a rejected call (Go errors are user-facing German text). */
export function errMsg(e: unknown): string {
  if (e instanceof Error && e.message) return e.message;
  if (typeof e === "string" && e) return e;
  return "Unerwarteter Fehler.";
}

/** Go nil slices arrive as null. */
export function list<T>(v: T[] | null | undefined): T[] {
  return v ?? [];
}

/** The structured error of a rejected call (Go UIError), if any. */
export interface CallCause {
  code?: string;
  message?: string;
  lock?: { host: string; user: string; since: string } | null;
}

export function errCause(e: unknown): CallCause | null {
  const c = (e as { cause?: unknown } | null)?.cause;
  return c && typeof c === "object" ? (c as CallCause) : null;
}

/** Another writer holds the archive (Go CodeLocked). */
export function isLocked(e: unknown): boolean {
  return errCause(e)?.code === "gesperrt";
}
