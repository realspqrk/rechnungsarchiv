VERDICT changes: 1

All five earlier findings are fixed. The offline TSA, stamp and export test suites passed with the testtsa tag.

1. Low — internal/stamp/stamp_test.go:330. The CRLF regression test ignores the error from writing its provider marker. If Windows denies that write, the original marker can remain intact, and a successful resend of attempt 1 makes the test pass without exercising CRLF handling. Check the marker read and write errors with t.Fatal before calling e.Stamp().
