VERDICT changes: 2

1. [P2] Do not infer that no list was fetched from an unconfirmed qualification — internal/report/statement.go:68.

   Scenario: an online check downloads a current, verified national list, but its historical service entry has no identity for the timestamp's date. The verifier deliberately returns Unconfirmed in this case (see internal/trust/qualify.go and TestQualifyHistoryWithoutIdentity). The PDF nevertheless states “bei dieser Kontrolle wurde keine aktuelle Liste abgerufen”, contradicting the actual check and the timestamp details' “bei dieser Kontrolle abgerufen”. Reproduced with a temporary overlay using an Unconfirmed token whose TrustList.FetchedOnline is true and whose issue date is after genTime.

   Fix: remove this unconditional suffix, or derive it from the actual fetch evidence. Preserve the distinction between an insufficiently recent list and a list that cannot establish the historical service identity. Add a regression covering online Unconfirmed, not only an old offline list.

2. [P2] Keep every deviation visible in the collapsed file table — app/frontend/src/views/deposit.ts:140.

   Scenario: an Ablage has 11 changed/missing/added files. deviationsFirst correctly moves them ahead of unchanged files, but the unconditional slice(0, FIRST_FILES) still hides the eleventh deviation behind “Alle … anzeigen”. For 11–20 deviations this also removes paths that the previous ChangedPaths list displayed immediately. The stated requirement that deviating files remain visible is therefore only met when there are at most ten.

   Fix: make the collapsed range include all deviations, adding unchanged files only to fill the usual ten-row preview, and show the expansion button only when unchanged rows remain hidden. Cover more than ten deviations in the frontend checks.

Validation: reviewed the diff against 9f15308, generated bindings, file mapping, qualification explanations, positive revocation wording, support-link allowlist, and supplied long-name PDF pages. The long names and paths render completely. Existing report tests, including TestLongFileNamesFitTheColumn, pass.

Gates: go vet ./..., gofmt -l ., git diff --check, npx.cmd tsc --noEmit, and npm.cmd test passed. go test -tags testtsa ./... passed every package except internal/creds/TestSystem, which cannot access Windows Credential Manager in this session (“no logon session”). Go used a temporary cache and GOFLAGS=-buildvcs=false for the environment's cache permissions and Git ownership. The additional overlay regression intentionally fails on finding 1. No repository files were edited.
