# ID Austria signing as a proof of archiving (research 2026-11-18)

Source: research subagent (a-trust.at, oesterreich.gv.at, the PDF-AS source code, RTR). The flow under review: the app generates an "Archivprotokoll" PDF containing the Prüfwert, the user signs it with ID Austria in a free online portal, then drops the signed PDF back into the app, which verifies it.

## Verdict

**ID Austria cannot replace a qualified timestamp.** At most it is an optional extra ("Freigabe durch Geschäftsführung", i.e. management sign-off).

| | ID Austria QES via free portal | Qualified RFC 3161 timestamp |
|---|---|---|
| Integrity | yes (§294 ZPO presumption; current SVG §4 still to be checked in RIS) | yes, eIDAS Art. 41(2) presumption |
| **Time T** | **only claimed** (`signingTime`/`/M`), no presumption | **presumed** |
| Who | yes | no |
| After certificate expiry (**ID Austria certificates are valid 5 years**, BAO requires 7) | INDETERMINATE without a timestamp | verifiable at genTime with the stored chain |
| Cost / terms | free **only for private individuals** (A-Trust: "ausschließlich für Privatpersonen kostenlos"); business use means a paid package, so this is a grey zone | ~€10 one-off |
| Effort | 2–4 minutes manual round trip per Ablage | automatic |
| Verification | PDF/CMS/ByteRange + prefix check + A-Trust chain | one CMS token over 32 bytes |

## Findings

- **Free portals:**
  - oesterreich.gv.at "PDF Signatur" (secure.oesterreich.gv.at). No terms of use found.
  - A-Trust PDF Sign: private use only, **no qualified timestamp** ("Im PDF Sign Tool wird kein qualifizierten Zeitstempel angebracht").
  - A-Trust Konto: does add a qualified timestamp, but is private use only.
  - EGIZ pdf.egiz.gv.at returned 403.
- **Signature profile:** the PDF-AS source (A-SIT, EUPL) has no TSA code → PAdES B-B, no LTV. The backend behind oesterreich.gv.at is not confirmed.
- **Byte preservation:** PDF-AS appends via `saveIncremental`, so the original PDF stays an exact prefix. A prefix check would be feasible.
- **Integration (API):** needs an A-Trust service provider contract; credentials may only be entered on A-Trust pages.
- **Go verification:** `digitorus/pdfsign` (BSD-2) can serve as a base. Revocation checks are online only, with no B-LT/LTA support.
- **Legal:**
  - A QES equals a handwritten signature (Art. 25(2)).
  - Art. 26(d) only requires that changes be detectable.
  - **A QES gives no presumption of time.** Only Art. 41(2) does that.
- **No existing Austrian archiving product signs archives with ID Austria.**

## Consequence

- Core mechanism stays the qualified RFC 3161 timestamp over the Prüfwert (ADR-0001).
- ID Austria approval of the Archivprotokoll goes to followup, as an optional extra. It would then be timestamped as well.

## Sources

- A-Trust Security Layer: <https://docs.a-trust.at/apis/SecurityLayer/>
- A-Trust commercial use: <https://a-trust.at/en/products/qualified_e-signature_services/commercial_use/>
- A-Trust PDF Sign: <https://www.a-trust.at/pdfsign>
- A-Trust Konto: <https://www.a-trust.at/konto/>
- oesterreich.gv.at PDF signing: <https://www.oesterreich.gv.at/landingpages/pdf_signatur_services>
- PDF-AS: <https://github.com/a-sit/pdf-as>
- PDF-Over: <https://github.com/a-sit/PDF-Over>
- digitorus/pdfsign: <https://github.com/digitorus/pdfsign>
- RTR signature verification: <https://www2.rtr.at/en/vd/Pruefung_Details>
