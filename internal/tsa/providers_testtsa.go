//go:build testtsa

package tsa

// RegisterTestProvider adds a provider (e.g. a local test TSA) to
// Providers(). It exists only in builds with the testtsa tag, so shipped
// binaries list qualified EU providers only.
func RegisterTestProvider(p Provider) { testProviders = append(testProviders, p) }

// AllowHTTP lets a client talk plain HTTP (local test TSAs).
func (c *Client) AllowHTTP() { c.allowHTTP = true }
