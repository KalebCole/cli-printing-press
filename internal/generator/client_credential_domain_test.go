package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBrowserCredentialBindsToCapturedDomain(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("credentialdomain")
	apiSpec.BaseURL = "https://api.credential-free.example"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "composed",
		Header:       "Authorization",
		Format:       "Bearer {token}",
		CookieDomain: ".auth.example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{"CREDENTIALDOMAIN_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "credentialdomain-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	goMod := readGeneratedFile(t, outputDir, "go.mod")

	assert.Contains(t, configSrc, "CredentialDomain",
		"browser-session config must persist the captured credential domain")
	assert.Contains(t, configSrc, `toml:"credential_domain,omitempty"`,
		"browser-session config must serialize the captured credential domain")
	assert.Contains(t, configSrc, "CredentialDomain: c.CredentialDomain",
		"persisted config must carry the captured credential domain")
	assert.Contains(t, authSrc, `cfg.CredentialDomain = ".auth.example.com"`,
		"browser login and refresh must bind the stored credential to the capture domain")
	assert.Contains(t, clientSrc, `seedBaseURL := "https://" + strings.TrimPrefix(cfg.CredentialDomain, ".")`,
		"cookie seeding must use the captured domain instead of the merged BaseURL")
	assert.Contains(t, clientSrc, "SeedCookieJarForDomain(cookieJar, seedBaseURL, cfg.CookieCredential(), cfg.CredentialDomain)",
		"cookie seeding must preserve the captured domain across its subdomains")
	assert.Contains(t, clientSrc, "credentialAllowed := c.credentialAppliesToURL(targetURL)",
		"requests must check the captured credential domain before injection")
	assert.Contains(t, clientSrc, "if authHeader != \"\" && credentialAllowed {",
		"the primary auth header must be withheld from unrelated hosts")
	assert.Contains(t, clientSrc, "req.URL.Host == via[0].URL.Host && c.credentialAppliesToURL(req.URL.String())",
		"same-host redirect re-stamping must retain the credential-domain gate")
	assert.Contains(t, clientSrc, "publicsuffix.EffectiveTLDPlusOne",
		"credential matching must use registrable domains")
	assert.Contains(t, goMod, "golang.org/x/net v0.55.0",
		"browser-session clients must declare the publicsuffix dependency")

	// The negative path must remain unchanged for ordinary token auth: it has
	// no capture-time domain to bind and should not gain browser-only baggage.
	bearer := minimalSpec("credentialdomain-bearer")
	bearerDir := filepath.Join(t.TempDir(), "credentialdomain-bearer-pp-cli")
	require.NoError(t, New(bearer, bearerDir).Generate())
	bearerClient := readGeneratedFile(t, bearerDir, "internal", "client", "client.go")
	bearerMod := readGeneratedFile(t, bearerDir, "go.mod")
	assert.NotContains(t, bearerClient, "credentialAppliesToURL",
		"ordinary token auth must not emit browser credential binding")
	assert.NotContains(t, bearerMod, "golang.org/x/net v0.55.0",
		"ordinary token auth must not gain the browser-only dependency")
	assert.Equal(t, 1, strings.Count(authSrc, `cfg.CredentialDomain = ".auth.example.com"`),
		"browser login should record the capture domain")

	runtimeTest := `package client

import (
	"testing"

	"credentialdomain-pp-cli/internal/config"
)

func TestCredentialAppliesToURL(t *testing.T) {
	c := &Client{Config: &config.Config{CredentialDomain: ".auth.example.com"}}
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "captured host", url: "https://login.auth.example.com/session", want: true},
		{name: "same registrable domain", url: "https://api.auth.example.com/items", want: true},
		{name: "credential-free host", url: "https://api.credential-free.example/items", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.credentialAppliesToURL(tc.url); got != tc.want {
				t.Fatalf("credentialAppliesToURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	if !c.credentialAppliesToURL("https://api.credential-free.example/items") {
		t.Fatal("verify-live HTTP must preserve mock-host credential behavior")
	}
}
`
	runtimePath := filepath.Join(outputDir, "internal", "client", "credential_domain_runtime_test.go")
	require.NoError(t, os.WriteFile(runtimePath, []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestCredentialAppliesToURL$", "-count=1")
}
