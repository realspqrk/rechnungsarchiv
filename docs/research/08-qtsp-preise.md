# Qualified RFC 3161 timestamps at low volume (research 2026-11-18)

Source: research subagent, providers' official price pages.

**Trusted List check (2026-11-18, official TSL XML via the EU LOTL):**
- **Mensatek** (ASETEC INGENIERIA DE SISTEMAS, S.L., `https://tsl.digital.gob.es/TSL.xml`): service type QTST, "MENSATEK QUALIFIED TSU01 - 2026", **granted** since 2026-11-04. The previous TSU01 is withdrawn as of the same date (certificate rotation).
- **Disig** (`http://tl.nbu.gov.sk/kca/tsl/tsl.xml`): several QTST services **granted**, including "(198) CA Disig Qualified TSA 1" (granted since 2026-08-11).
- Also checked: **Aruba**, **Namirial**, **InfoCert** (IT TL), **Certigna** (FR TL), **GLOBALTRUST** and **A-Trust** (AT TL): each has QTST services with status **granted**. Universign was not found under that name; its status is unverified.
- The TSL entries contain the services' certificates (ServiceDigitalIdentity). These become our pinned trust anchors for "qualifiziert" (ADR-0004).

**Still open:** whether these providers sell to Austrian companies, and whether EU B2B reverse charge applies to VAT.

Prices are for ~50 stamps per year:

| # | Provider | Smallest package (verbatim) | Validity | Protocol / auth | ≈ €/year for 50 (net) |
|---|---|---|---|---|---|
| 1 | **Mensatek / ASETEC (ES)** | "From €10, for any amount you like"; €10 = 233 credits (€0.043/credit, checked in the customer area 2026-11-18), 1 credit/stamp; invoiced to an AT company with UID as "Exento de IVA" (reverse charge); customer area Spanish only | **"They never expire"** | RFC 3161 HTTPS POST, **HTTP Basic** or client certificate | **~€2** (a €10 top-up lasts for years) |
| 2 | InfoCert via Openapi.com (IT) | "100: €9.70 + VAT" | 10 years | Openapi JSON API, unclear whether raw RFC 3161 is offered | ~€9.70 |
| 3 | **Disig (SK)** | "TS 100: 11,67 € without VAT" | ≥365 days, token valid 3 years | RFC 3161 HTTP(S); auth via access-token certificate; e-shop | €11.67 |
| 4 | Aruba (IT) | "Pack of 50: €13.50 + VAT" | not stated | user/password; endpoint known only from third-party guides | €13.50 |
| 5 | Namirial (IT) | "Pack of 100: 25,00€ +VAT" | never expire | RFC 3161 Basic, credentials via sales | €25 |
| 6 | Certigna (FR) | "100 tokens: 36 € HT" | 1 year | RFC 3161 API | €36 |
| 7 | Universign (FR) | "Pack of 100: 39€" | 1 year | API on request | €39 |
| 8 | GLOBALTRUST (AT) | "1.000 … EUR 185,00/Jahr" | expires at year end | RFC 3161 | €185 |

**Not suitable / no price found:**
- **A-Trust:** free only for private individuals signing via their A-Trust account; for businesses: "contact sales".
- **Certum, D-Trust, Sectigo, Swisscom, Buypass, Evrotrust:** price only on request or by contract.
- **SK ID Solutions:** ≥ ~€36/month.
- **Free public TSAs:** Slovakia's NASES TSA is restricted to e-government use. Slovenia's SI-TSA states no terms.

**Consequence:** the qualified timestamp no longer has a cost problem. It is a generic RFC 3161 client with HTTP Basic auth, and the provider is just configuration. Mensatek is the cheapest; Disig is the fallback.

## Sources

- <https://www.mensatek.com/en/precios.php>
- <https://www.mensatek.com/api-sellado-de-tiempo-en.html>
- <https://www.mensatek.com/psc/MST_PDS_TSU_EN_v1.r4.pdf>
- <http://eidas.disig.sk/en/qualified-electronic-time-stamps/pricelist/>
- <https://eidas.disig.sk/en/timestamp/>
- <https://openapi.com/products/infocert-timestamps>
- <https://www.aruba.it/en/time-stamping-price-list.aspx>
- <https://www.namirial.global/?p=372>
- <https://www.certigna.com/famille-de-services/horodatage/>
- <https://universign.com/en/prices>
- <https://globaltrust.eu/produkte/qualified-timestamp/>
- <https://www.a-trust.at/en/products/signature_solutions_for_companies/timestamp_and_ltv>
- <https://www.slovensko.sk/_img/CMS4/Podmienky_pouzivania_sluzby-NASES-TSA.pdf>

## Update 2026-11-18 (afternoon): German-language options

Austrian QTST services on the AT list: A-Trust (3 granted), GLOBALTRUST (2 granted, 1 withdrawn), Swisscom IT Services Finance S.E. (Vienna, 1 granted).

| Provider | Price for 20–100/yr | German | Self-service | Access |
|---|---|---|---|---|
| **DGN Deutsches Gesundheitsnetz Service GmbH** (DE list: "DGN Zeitstempelservice" granted) | "0-5 Zeitstempel 0,00 €/Stück", "6-99 Zeitstempel 0,09 €/Stück", "5 Zeitstempel/Monat kostenlos", "Keine monatlichen Gebühren - keine Mindestabnahme" | yes | free online registration | RFC 3161 over HTTPS, "ausschließlich über einen authentifizierten Zugriff"; method to confirm |
| GLOBALTRUST (AT) | 1,000/yr EUR 185; the order form only offers "individuelles Angebot"; best-price guarantee "Differenz + 10%" | yes | quote form | user/password |
| A-Trust (AT) | no business price published | yes | sales contact | unknown |
| Swisscom (AT entity) | no price; smallest package 1,800 units/yr | yes | request an offer | unknown |
| D-Trust (DE) | no price | yes | no | client certificate |
| AlfaTrust (RO) | 6,000 for EUR 250 over 24 months | no | yes | unknown |

**Consequence:** DGN is the most suitable for German-speaking SMEs (free at a monthly rhythm) if it accepts Austrian customers and offers user/password access. Still to confirm by registering: endpoint URL, auth, AT customers, billing.

Sources:
- <https://www.dgn.de/dgn-zeitstempeldienst/>
- <https://www.dgn.de/support/article/dgn-preisblatt/>
- <https://globaltrust.eu/produkte/qualified-timestamp/>
- <https://order.globaltrust.eu/php/formservice.php?form=order_timestamp>
- <https://www.a-trust.at/de/produkte/signaturlösungen_für_unternehmen/zeitstempel_und_ltv/>
- <https://trustservices.swisscom.com/en/products/service-packages>
- <https://qtsa.eu/>
- DE trusted list: <https://tl.bundesnetzagentur.de/TL-DE.xml>
