VERDICT changes: 1

1. [P2] Wrap the PDF header so valid names remain visible — internal/report/report.go:393–395.

   Scenario: archive a folder with a valid 180-character name, or use a long company name. The new header draws each subtitle directly on one line, replacing the previous wrapping layout. At 11 pt, a name consisting of 180 W characters measures 1,867 pt against the available 762 pt. The rendered PDF clips the name at the right page edge and loses the following Belegjahr entirely. Long company names are similarly clipped. These inputs are accepted by the archive validation; company names have no length cap. Text() still records the full strings, so content assertions do not reveal the missing visible information.

   Suggested fix: wrap both subtitle strings to textW, advance the vertical position for every resulting line, and handle page breaks before drawing. Add a regression using an accepted long Ablage name and company name that checks rendered bounds or PDF extraction, including the Belegjahr. Reproduced using a temporary Go overlay, with long-header.pdf and header.png beside this report; repository files were not edited.

Validation: reviewed the uncommitted diff against 8596bbe and all three new Explorer files. Reviewed status statements, inherited proof handling, key-compromise wording, disclaimer parsing, folder-name blocking, and pagination. The long-path rendering retained a 1,013-byte path and repeated table headers. Existing report tests, including deterministic PDF generation and extraction order, passed.

Gates: go vet ./..., gofmt -l ., npx.cmd tsc --noEmit, and npm.cmd test passed. go test -tags testtsa ./... passed all packages except cmd/rechnungsarchiv (environmental Git ownership/VCS stamping error) and internal/creds (Windows Credential Manager: no logon session). The CLI package passed on retry with GOFLAGS=-buildvcs=false. Go checks used a temporary cache after the default cache denied access. No repository files were changed.
