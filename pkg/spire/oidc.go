// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"time"
)

var (
	errNoPEM          = errors.New("no PEM block found")
	errUnknownKeyType = errors.New("unsupported private key type")
	errCABundle       = errors.New("no valid certificate found in the CA PEM")
	errNoKeys         = errors.New("the key set is empty")
)

// maxJWKSSize bounds the key set read from the provider.
const maxJWKSSize = 1 << 20

// SignServingCert generates a key pair and a certificate for the SPIRE OIDC discovery provider, signed by the given
// CA. The certificate is valid for hostname, address and 127.0.0.1. Both outputs are PEM encoded (the key as PKCS#8).
func SignServingCert(caCertPEM, caKeyPEM []byte, hostname string, address net.IP, validity time.Duration,
) ([]byte, []byte, error) {
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		return nil, nil, fmt.Errorf("CA certificate: %w", errNoPEM)
	}

	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse the CA certificate: %w", err)
	}

	caKey, err := parsePrivateKey(caKeyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse the CA key: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if hostIP := net.ParseIP(hostname); hostIP != nil {
		template.IPAddresses = append(template.IPAddresses, hostIP)
	} else {
		template.DNSNames = append(template.DNSNames, hostname)
	}

	if address != nil && !address.Equal(net.ParseIP(hostname)) {
		template.IPAddresses = append(template.IPAddresses, address)
	}

	// Consul servers validate the JWT-SVIDs against the provider of their own host, through the loopback address.
	if !containsIP(template.IPAddresses, net.IPv4(127, 0, 0, 1)) {
		template.IPAddresses = append(template.IPAddresses, net.IPv4(127, 0, 0, 1))
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to sign the OIDC serving certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		nil
}

func containsIP(ips []net.IP, ip net.IP) bool {
	for _, candidate := range ips {
		if candidate.Equal(ip) {
			return true
		}
	}

	return false
}

func parsePrivateKey(keyPEM []byte) (any, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errNoPEM
	}

	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}

		switch key.(type) {
		case *ecdsa.PrivateKey, *rsa.PrivateKey, ed25519.PrivateKey:
			return key, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", errUnknownKeyType, block.Type)
}

// WaitJWKS blocks until the OIDC discovery provider serves at least one key at jwksURL, trusting only caPEM, or the
// timeout expires. A key is served once the provider has read the trust bundle from its SPIRE server.
func WaitJWKS(jwksURL string, caPEM []byte, timeout, interval time.Duration) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return errCABundle
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DisableKeepAlives: true,
		},
	}
	defer client.CloseIdleConnections()

	deadline := time.Now().Add(timeout)

	for {
		err := fetchKeys(client, jwksURL)
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout reached while waiting for the OIDC discovery provider at %s: %w", jwksURL, err)
		}

		time.Sleep(interval)
	}
}

func fetchKeys(client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status) //nolint:err113 // transient, only reported on timeout
	}

	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}

	err = json.NewDecoder(io.LimitReader(resp.Body, maxJWKSSize)).Decode(&jwks)
	if err != nil {
		return fmt.Errorf("unable to decode the key set: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return errNoKeys
	}

	return nil
}
