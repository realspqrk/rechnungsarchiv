# ADR-0006: Retention

**Status:** accepted 2026-11-18. Revised after review round 1.

## Context
- **§132 Abs 1 BAO:** 7 years from the end of the calendar year the document relates to.
  - If the financial year differs from the calendar year, the period starts at the end of the calendar year in which the financial year ends.
  - Pending proceedings extend the period.
- **§18 Abs 10 UStG:** 22 years for real estate (Grundstücke); 12 years for certain WGG cases.
- §207 BAO (limitation period) is **not** a retention period. Keeping documents for 10 years is a voluntary precaution.
- GDPR Art. 5(1)(e): documents must be deleted once their period has ended.

## Decision
- The **Belegjahr** (document year) is pre-filled from the folder name (`2026-11 …` → 2026). If the name doesn't contain a year, the user enters it. The tool warns if the file dates span several years. There is no "Nachträglich archiviert" (archived late) label, because the dates already show this.
- **Retention period:** chosen once in the settings; the dialog offers an override.

  | Option | Period |
  |---|---|
  | `7` | statutory, the default |
  | `10` | voluntary |
  | `12` | WGG |
  | `22` | real estate (Grundstücke) |

- It is stored in the bag as `Aufbewahrung-Jahre` and `Aufbewahrung-Bis` (31.12. of Belegjahr + N).
- **Not in v1:** deletion and the "Sperre" (hold) flag (cut as YAGNI). The format still allows deletion later, with an authenticated tombstone (`followup.md`).
