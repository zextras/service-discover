package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// The JWKS endpoint of the SPIRE OIDC discovery provider may not be serving yet when Consul starts at boot:
	// Consul validates the auto_config authorization keys at startup and refuses to start if it cannot fetch them.
	jwksWaitWindow   = 60 * time.Second
	jwksPollInterval = 2 * time.Second
	jwksHTTPTimeout  = 5 * time.Second
)

// jwksSource is the endpoint serving the keys that Consul uses to validate the auto_config JWT-SVIDs.
type jwksSource struct {
	url   string
	caPEM string
}

// readJWKSSource returns the auto_config authorization keys endpoint of the Consul configuration, if configured.
// A configuration that cannot be read or parsed has none: the regular startup reports the problem.
func readJWKSSource(configPath string) (jwksSource, bool) {
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // path of the service configuration
	if err != nil {
		return jwksSource{}, false
	}

	var cfg struct {
		AutoConfig struct {
			Authorization struct {
				Enabled bool `json:"enabled"`
				Static  struct {
					JWKSURL    string `json:"jwks_url"`
					JWKSCACert string `json:"jwks_ca_cert"`
				} `json:"static"`
			} `json:"authorization"`
		} `json:"auto_config"`
	}

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return jwksSource{}, false
	}

	authorization := cfg.AutoConfig.Authorization
	if !authorization.Enabled || authorization.Static.JWKSURL == "" {
		return jwksSource{}, false
	}

	return jwksSource{url: authorization.Static.JWKSURL, caPEM: authorization.Static.JWKSCACert}, true
}

// waitForJWKS blocks until the auto_config authorization keys endpoint of the Consul configuration answers 200,
// polling every interval for at most window. It gives up with a log line when the window elapses: Consul is started
// anyway, it reports the problem itself. Without auto_config authorization it returns at once.
func waitForJWKS(deps deps, configPath string, window, interval time.Duration) {
	source, ok := readJWKSSource(configPath)
	if !ok {
		return
	}

	client := newJWKSClient(source.caPEM)
	deadline := deps.Now().Add(window)

	var lastErr error

	for attempt := 1; ; attempt++ {
		lastErr = fetchJWKS(client, source.url)
		if lastErr == nil {
			if attempt > 1 {
				deps.Log(fmt.Sprintf("auto_config keys %s available after %d attempts\n", source.url, attempt))
			}

			return
		}

		remaining := deadline.Sub(deps.Now())
		if remaining <= 0 {
			deps.Log(fmt.Sprintf("auto_config keys %s still unavailable after %s (%s), starting consul anyway\n",
				source.url, window, lastErr))

			return
		}

		if attempt == 1 {
			deps.Log(fmt.Sprintf("waiting for the auto_config keys %s (up to %s): %s\n", source.url, window, lastErr))
		}

		deps.Sleep(min(interval, remaining))
	}
}

// newJWKSClient builds an HTTP client verifying the endpoint against the CA Consul is configured with, as Consul does.
// Without a CA the system roots are used.
func newJWKSClient(caPEM string) *http.Client {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec // false positive: TLS 1.2 is the minimum

	if caPEM != "" {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM([]byte(caPEM)) {
			tlsConfig.RootCAs = pool
		}
	}

	return &http.Client{
		Timeout:   jwksHTTPTimeout,
		Transport: &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true},
	}
}

func fetchJWKS(client *http.Client, url string) error {
	response, err := client.Get(url) //nolint:noctx // bounded by the client timeout
	if err != nil {
		return fmt.Errorf("get keys: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("get keys: %s", response.Status)
	}

	return nil
}
