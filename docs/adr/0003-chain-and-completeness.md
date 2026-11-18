# ADR-0003: Numbering, chain, single writer

**Status:** accepted 2026-11-18. Revised after review round 1. Normative: [`spec.md`](../spec.md) §1, §6 (steps 8–9).

## Decision
- `chain.json` holds `archive_id`, `sequence` and `previous`, and is covered by the Prüfwert.
- `verify` reports:
  - **Lücke** (gap): a missing number;
  - **Nummer doppelt vergeben** (number used twice): a fork, typically from a copied archive.

  The wording names the actual mistake rather than accusing the user of manipulation.
- **Proven date via the chain.** Each successor commits to its predecessor, so an Ablage counts as proven no later than the earliest valid timestamp of itself or of any later Ablage. This shortens the window for Ablagen that were archived offline.
- **Only one PC writes.** The `.lock` records host, user and time. A lock held by another PC can only be taken over after confirmation. Synced or cloud folders are not supported as a write target; the docs say "never copy the archive to work in it".
- **Not in v1:** an external completeness checkpoint. The docs state plainly that deleting the *most recent* Ablage cannot be detected from local data alone.
