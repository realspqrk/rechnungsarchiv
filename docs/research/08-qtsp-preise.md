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
| 1 | **Mensatek / ASETEC (ES)** | "From €10, for any amount you like"; €0.042/credit, 1 credit/stamp; excl. VAT | **"They never expire"** | RFC 3161 HTTPS POST, **HTTP Basic** or client certificate | **~€2** (a €10 top-up lasts for years) |
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
