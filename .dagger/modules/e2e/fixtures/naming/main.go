// Declarations whose schema names differ between the engine's naming rules
// and the legacy strcase rules, called through the module's own client.
package naming_app

import (
	"context"
	"strings"

	"dagger/naming-app/internal/dagger"
)

type NamingApp struct{}

// Arch's values are ARM_64 and AMD_64 by the strcase rules and ARM64 and
// AMD64 by the engine's.
type Arch string

const (
	ArchArm64 Arch = "arm64"
	ArchAmd64 Arch = "amd64"
)

// HttpClient is NamingAppHttpClient by the strcase rules and
// NamingAppHTTPClient by the engine's.
type HttpClient struct {
	BaseUrl string
}

func (m *NamingApp) HttpClient(baseUrl string) *HttpClient {
	return &HttpClient{BaseUrl: baseUrl}
}

func (c *HttpClient) GetUrl(path string) string {
	return c.BaseUrl + path
}

// E2ETest is e2Etest by the strcase rules and e2eTest by the engine's.
func (m *NamingApp) E2ETest() string {
	return "e2e"
}

func (m *NamingApp) Platform(arch Arch) string {
	return string(arch)
}

// HttpFetcher is NamingAppHttpFetcher by the strcase rules and
// NamingAppHTTPFetcher by the engine's. Calls through it select the
// interface by that name.
type HttpFetcher interface {
	dagger.DaggerObject
	GetUrl(ctx context.Context, path string) (string, error)
}

func (m *NamingApp) Fetch(ctx context.Context, fetcher HttpFetcher, path string) (string, error) {
	return fetcher.GetUrl(ctx, path)
}

// SelfCall calls the module's own functions through the engine, so the
// schema names the bindings predict are on the wire: the enum member and the
// namespaced interface the fetch goes through. Their Go identifiers are the
// same by either rules.
func (m *NamingApp) SelfCall(ctx context.Context) (string, error) {
	platform, err := dag.NamingApp().Platform(ctx, dagger.NamingAppArchArm64)
	if err != nil {
		return "", err
	}
	url, err := dag.NamingApp().Fetch(ctx, dag.NamingApp().HTTPClient("https://example.com"), "/x")
	if err != nil {
		return "", err
	}
	return strings.Join([]string{platform, url}, " "), nil
}
