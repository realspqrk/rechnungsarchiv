VERDICT changes: 5

1. High — internal/stamp/stamp.go:355. A restored or modified unanswered request can carry extra data to the provider. ParseRequest checks the imprint, policy and presence of a positive nonce, but accepts extensions, arbitrary algorithm parameters and oversized nonces. Send posts the original DER verbatim. Before reuse, require byte equality with the application's canonical request encoding for that imprint, policy and stored nonce, enforcing the 64-bit nonce limit and certReq=true. On mismatch, generate a fresh attempt. Cover this with a regression test that checks what reaches the test TSA.

2. Medium — internal/stamp/stamp.go:324. Attempt selection compares numbers across all matching folders, but allocation uses only p.dir. Suppose the hint folder ends at 1 and another matching folder holds attempt 12 for a previous provider. On a 402 the new provider writes attempt 2 in the hint folder. The next run selects 12 again and writes 3, recreating the clutter. Allocate fresh attempts above both the largest number paired with this imprint and the largest occupied number in the destination folder. Test two successive refusals after a provider change with split folders.

3. Medium — internal/stamp/stamp.go:348. An existing provider marker ending in a newline passes TrimSpace, but send calls writeOnce with the bare ID. The byte comparison fails, so every retry stops before contacting the TSA. On reuse, send the request after validating the existing marker, and reserve writing the marker for a new attempt. Add a case with a CRLF-terminated marker.

4. Low — internal/stamp/stamp.go:320. If the first HTTP response is lost and the provider returns that original token on a resend ten minutes later, its nonce and imprint correctly match. The existing skew warning nevertheless tells the user their PC clock is wrong. On a resend, describe the time difference neutrally and mention that the token can date from the earlier submission. Update the time-sanity wording in §5. Keep evidence deadlines relative to the signed genTime.

5. Low — README.md:83. Topping up while the app is idle leaves the Ablage pending until another stamp run. “Sobald wieder Guthaben da ist” tells the reader they can simply wait. Tell them to click “Jetzt nachholen” after topping up, or that the next program start retries automatically.

The offline testtsa suites for internal/stamp and internal/export passed.
