// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/consul/api/v2"
)

const (
	docsEditorMetaKey = "service_id"
	docsEditorHCL     = "carbonio-docs-editor.hcl"
	docsEditorTpl     = "carbonio-docs-editor-template.hcl"

	alertmanagerKey = "carbonio-alertmanager/config"

	// commandTimeout bounds the commands of the packages: some wait for a key that a node has to publish first.
	commandTimeout = 10 * time.Minute

	turnServerTLSKeyMode = 0o640
	turnServerConfMode   = 0o640
	placeholderToken     = "{{ consultoken }}" //nolint:gosec // the name of a placeholder
	placeholderDomain    = "{{ hostsdomain }}"
	placeholderConsulDom = "{{ consulhostsdomain }}"
)

var serviceIDPattern = regexp.MustCompile(`service_id\s*=\s*"([^"]+)"`)

// prepareDocsEditor generates the service definition of docs-editor from the template of the package, as its setup
// did with carbonio-docs-routes-generator.py. The generator asks the catalog without a token, that a node using SPIRE
// is not allowed to do, so it is done here with the token of the service.
//
// Every instance of docs-editor has an identifier in its service metadata, that the routing uses to reach one
// instance in particular: the one of this node is the one the catalog has for it, otherwise the one the previous
// generation chose, otherwise a new one.
func prepareDocsEditor(ctx *setupContext) error {
	template, err := os.ReadFile(filepath.Join(ctx.env.ServicesDir, "templates", docsEditorTpl))
	if err != nil {
		return fmt.Errorf("unable to read the template of the service definition: %w", err)
	}

	consul, err := ctx.env.Consul(ctx.token)
	if err != nil {
		return err
	}

	instances, err := consul.Instances(ctx.def.Name, false)
	if err != nil {
		return fmt.Errorf("unable to read the instances of %s: %w", ctx.def.Name, err)
	}

	target := filepath.Join(ctx.env.ServicesDir, docsEditorHCL)

	identifier := localServiceID(ctx.env, instances)
	if identifier == "" {
		identifier = generatedServiceID(target)
	}

	if identifier == "" {
		identifier = uuid.NewString()
	}

	replacer := strings.NewReplacer("$METADATA_KEY", docsEditorMetaKey, "$METADATA_VALUE", identifier)
	content := replacer.Replace(string(template))

	err = ensureOwnedFile(ctx.env, target, []byte(content), 0o644, Account{})
	if err != nil {
		return err
	}

	ctx.log("%s: service definition generated in %s", ctx.def.Name, target)

	return nil
}

// finishDocsEditor writes the router and the resolver sending a request to the instance whose identifier it carries
// in its query string.
func finishDocsEditor(ctx *setupContext) error {
	consul, err := ctx.env.Consul(ctx.token)
	if err != nil {
		return err
	}

	instances, err := consul.Instances(ctx.def.Name, false)
	if err != nil {
		return fmt.Errorf("unable to read the instances of %s: %w", ctx.def.Name, err)
	}

	var ids []string

	for _, instance := range instances {
		if id := instance.Meta[docsEditorMetaKey]; id != "" {
			ids = appendUnique(ids, id)
		}
	}

	if id := generatedServiceID(filepath.Join(ctx.env.ServicesDir, docsEditorHCL)); id != "" {
		ids = appendUnique(ids, id)
	}

	slices.Sort(ids)

	return writeRouting(consul, ctx.def.Name, ids, func(id string) api.ServiceRouteHTTPMatch {
		return api.ServiceRouteHTTPMatch{
			QueryParam: []api.ServiceRouteHTTPMatchQueryParam{{Name: docsEditorMetaKey, Exact: id}},
		}
	}, docsEditorMetaKey)
}

// writeRouting writes the resolver, with a subset for each instance identifier, and the router sending to a subset
// the requests matching it.
func writeRouting(
	consul Consul,
	name string,
	ids []string,
	match func(id string) api.ServiceRouteHTTPMatch,
	metaKey string,
) error {
	if len(ids) == 0 {
		return nil
	}

	resolver := &api.ServiceResolverConfigEntry{
		Kind:    api.ServiceResolver,
		Name:    name,
		Subsets: map[string]api.ServiceResolverSubset{},
	}
	router := &api.ServiceRouterConfigEntry{Kind: api.ServiceRouter, Name: name}

	for _, identifier := range ids {
		filter := fmt.Sprintf("Service.Meta.%s == %q", metaKey, identifier)
		resolver.Subsets[identifier] = api.ServiceResolverSubset{Filter: filter}

		httpMatch := match(identifier)

		router.Routes = append(router.Routes, api.ServiceRoute{
			Match:       &api.ServiceRouteMatch{HTTP: &httpMatch},
			Destination: &api.ServiceRouteDestination{Service: name, ServiceSubset: identifier},
		})
	}

	err := consul.SetConfigEntry(resolver)
	if err != nil {
		return fmt.Errorf("unable to write the resolver of %s: %w", name, err)
	}

	err = consul.SetConfigEntry(router)
	if err != nil {
		return fmt.Errorf("unable to write the router of %s: %w", name, err)
	}

	return nil
}

func localServiceID(env *Env, instances []Instance) string {
	local := localAddresses(env)

	for _, instance := range instances {
		if id := instance.Meta[docsEditorMetaKey]; id != "" && local[instance.Address] {
			return id
		}
	}

	return ""
}

// generatedServiceID returns the identifier in a generated service definition, empty when there is none.
func generatedServiceID(path string) string {
	match := serviceIDPattern.FindSubmatch(readFileOrNil(path))
	if match == nil {
		return ""
	}

	return string(match[1])
}

// prepareTurnServer does what the setup of the coturn service does besides the mesh: it generates its configuration
// on the first installation, after checking the node can run it, and gives the TLS key to the user of the service.
func prepareTurnServer(ctx *setupContext) error {
	account, err := ctx.env.LookupUser(ctx.def.User)
	if err != nil {
		return err
	}

	conf := joinCarbonio(ctx.env, "turn-server", "turnserver.conf")

	if !isFile(conf) {
		err = generateTurnServerConf(ctx, conf, account)
		if err != nil {
			return err
		}

		ctx.log("%s: %s generated", ctx.def.Name, conf)
	}

	key := filepath.Join(ctx.env.CertDir, "commercial.key")

	err = ctx.env.Chown(key, account.UID, account.GID)
	if errors.Is(err, fs.ErrNotExist) {
		// The certificate is installed later: the setup, as before, goes on without it.
		ctx.log("%s: %s not found, its owner is left alone", ctx.def.Name, key)

		return nil
	}

	if err != nil {
		return fmt.Errorf("unable to give the TLS key to %s: %w", ctx.def.User, err)
	}

	return os.Chmod(key, turnServerTLSKeyMode)
}

func generateTurnServerConf(ctx *setupContext, conf string, account Account) error {
	env := ctx.env

	fqdn := hostnameOutput(env, "-f")
	if fqdn == "" || !strings.Contains(fqdn, ".") || strings.HasPrefix(fqdn, "localhost") || fqdn == "localdomain" {
		return fmt.Errorf("%w: hostname is not properly configured (got %q): set a valid FQDN with hostnamectl "+
			"set-hostname and update /etc/hosts accordingly, then run the pending setups again", errNoValue, fqdn)
	}

	err := checkCertificate(filepath.Join(env.CertDir, "commercial.crt"), fqdn)
	if err != nil {
		return err
	}

	secret := make([]byte, 32)

	_, err = rand.Read(secret)
	if err != nil {
		return err
	}

	local := localIP(env)

	public := debconfValue(env, "videoserver/public-ip")
	if public == "" {
		public = local
	}

	realm := hostnameOutput(env, "-d")
	if realm == "" {
		realm = fqdn
	}

	template, err := os.ReadFile(conf + ".template")
	if err != nil {
		return fmt.Errorf("unable to read the template of the configuration: %w", err)
	}

	content := strings.NewReplacer(
		"${TURN_SECRET}", hex.EncodeToString(secret),
		"${PUBLIC_IP}", public,
		"${LOCAL_IP}", local,
		"${REALM}", realm,
	).Replace(string(template))

	return writeFile(env, conf, []byte(content), turnServerConfMode, account)
}

// debconfValue returns the answer of a debconf question, empty when the node has no debconf or no answer.
func debconfValue(env *Env, question string) string {
	out, err := env.Exec(&Command{
		Name: "debconf-communicate", Args: []string{"videoserver"}, Stdin: "GET " + question + "\n",
	})
	if err != nil {
		return ""
	}

	code, value, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if code != "0" {
		return ""
	}

	return strings.TrimSpace(value)
}

// checkCertificate checks the certificate exists and covers the host name.
func checkCertificate(path, host string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: TLS certificate not found at %s, install the certificate first", errUnreadable, path)
	}

	if err != nil {
		return err
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return fmt.Errorf("%w: %s is not a PEM certificate", errUnreadable, path)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	err = cert.VerifyHostname(host)
	if err != nil && len(cert.DNSNames) == 0 && len(cert.IPAddresses) == 0 && matchesName(cert.Subject.CommonName, host) {
		err = nil
	}

	if err != nil {
		return fmt.Errorf("%w: %s does not cover the hostname %q (it covers %s): install a certificate "+
			"that covers this hostname, then run the pending setups again", errCertificate, path, host, certificateNames(cert))
	}

	return nil
}

// matchesName compares a certificate name, that may be a wildcard, with a host name.
func matchesName(pattern, host string) bool {
	if strings.EqualFold(pattern, host) {
		return true
	}

	suffix, wildcard := strings.CutPrefix(pattern, "*.")
	_, domain, found := strings.Cut(host, ".")

	return wildcard && found && strings.EqualFold(suffix, domain)
}

func certificateNames(cert *x509.Certificate) string {
	names := append([]string{cert.Subject.CommonName}, cert.DNSNames...)

	return strings.Join(names, ", ")
}

// prepareAlertmanager does what the setup of alertmanager does: the configuration shipped by the package is stored in
// the KV store, where the nodes find the configuration of the cluster, and the configuration file is rendered from it.
func prepareAlertmanager(ctx *setupContext) error {
	account, err := ctx.env.LookupUser(ctx.def.User)
	if err != nil {
		return err
	}

	source := joinCarbonio(ctx.env, ctx.def.Dir, "config-templates", "alertmanager.yml")
	target := joinCarbonio(ctx.env, ctx.def.Dir, "alertmanager.yml")

	config, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("unable to read the configuration of the package: %w", err)
	}

	consul, err := ctx.env.Consul(ctx.token)
	if err != nil {
		return err
	}

	err = consul.Put(alertmanagerKey, string(config))
	if err != nil {
		return fmt.Errorf("unable to store the configuration: %w", err)
	}

	stored, found, err := consul.Get(alertmanagerKey)
	if err != nil {
		return fmt.Errorf("unable to verify the stored configuration: %w", err)
	}

	if !found || stored != string(config) {
		return fmt.Errorf("%w: the stored configuration does not match", errNoValue)
	}

	// The configuration is checked before it replaces the working one.
	candidate := target + ".candidate"

	err = writeFile(ctx.env, candidate, []byte(stored), turnServerConfMode, account)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(candidate) }()

	_, err = ctx.env.Exec(&Command{Name: "amtool", Args: []string{"check-config", candidate}, Out: ctx.out})
	if err != nil {
		return fmt.Errorf("the configuration is not valid: %w", err)
	}

	return os.Rename(candidate, target)
}

// substitute fills the placeholders of the configuration files of the service that its package setup replaced in
// place, with the token and the domain of the node. A file that already got its token gets the new one when the token
// is replaced. It reports whether any file changed.
func substitute(env *Env, def *Definition, token, previous string, log func(string, ...any)) (bool, error) {
	changed := false

	var failures []error

	for _, name := range def.SubstituteFiles {
		file := joinCarbonio(env, name)

		raw, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			failures = append(failures, err)

			continue
		}

		replacements := []string{placeholderToken, token}

		if previous != "" && previous != token {
			replacements = append(replacements, previous, token)
		}

		domain := ""
		if bytes.Contains(raw, []byte(placeholderDomain)) || bytes.Contains(raw, []byte(placeholderConsulDom)) {
			domain = hostnameOutput(env, "-d")
		}

		if domain != "" {
			replacements = append(replacements, placeholderDomain, domain,
				placeholderConsulDom, strings.ReplaceAll(domain, ".", "-"))
		}

		edited := strings.NewReplacer(replacements...).Replace(string(raw))
		if edited == string(raw) {
			continue
		}

		err = rewriteInPlace(env, file, []byte(edited))
		if err != nil {
			failures = append(failures, err)

			continue
		}

		log("%s: placeholders replaced in %s", def.Name, file)

		changed = true
	}

	return changed, errors.Join(failures...)
}

// execCommand runs a command of the machine.
func execCommand(cmd *Command) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// The commands are the ones of the packages, named by the embedded definitions.
	run := exec.CommandContext(ctx, cmd.Name, cmd.Args...) //nolint:gosec
	run.Env = append(os.Environ(), cmd.Env...)

	if cmd.Stdin != "" {
		run.Stdin = strings.NewReader(cmd.Stdin)
	}

	if cmd.Out != nil {
		run.Stdout = cmd.Out
		run.Stderr = cmd.Out

		return nil, run.Run()
	}

	out, err := run.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	return out, nil
}
