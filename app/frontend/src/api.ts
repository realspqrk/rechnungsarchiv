// The generated Wails bindings are the only door to Go. Re-exported here so
// views import one module.
export { Shell as api } from "../bindings/github.com/realspqrk/rechnungsarchiv/app";
export type * from "../bindings/github.com/realspqrk/rechnungsarchiv/app";
export { OrdnerAbgelegt } from "../bindings/events";
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
