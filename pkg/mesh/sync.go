// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// ServicesDir is where the packages install the Consul service definitions, and the configuration of the agent.
	ServicesDir = "/etc/zextras/service-discover"
	// RenderedDir holds the service definitions with the token injected, loaded by Consul in place of the package
	// ones on the nodes using SPIRE.
	RenderedDir = "/var/lib/service-discover/services.d"
	// CarbonioDir is the parent of the directories of the packages.
	CarbonioDir = "/etc/carbonio"
	// SystemdDir is where the drop-ins of the administrator live.
	SystemdDir = "/etc/systemd/system"
	// CertDir holds the TLS certificate and key of the node.
	CertDir = "/opt/zextras/ssl/carbonio/commercial"
	// ConsulUnit is the unit of the Consul agent, server or client.
	ConsulUnit = "service-discover.service"
	// ConsulUser is the user the Consul agent runs as.
	ConsulUser = "service-discover"

	tokenFileMode   = 0o600
	renderedMode    = 0o600
	dropInMode      = 0o600
	directoryMode   = 0o755
	renderedDirMode = 0o750
)

// Account is the identity of a system user.
type Account struct {
	UID int
	GID int
}

// Env is everything Sync needs from the machine and the cluster. Production code uses NewEnv, tests substitute fakes.
type Env struct {
	// ServicesDir, RenderedDir, CarbonioDir and SystemdDir are the locations described by the constants.
	ServicesDir string
	RenderedDir string
	CarbonioDir string
	SystemdDir  string
	CertDir     string
	TrustDomain string

	// LookupUser returns the account of a user, fs.ErrNotExist when the user does not exist.
	LookupUser func(name string) (Account, error)
	// Chown changes the owner of a path.
	Chown func(path string, uid, gid int) error
	// FetchJWT returns a JWT-SVID of spiffeID, requested as the given account: the identity is granted to the
	// processes of that user, so that is the only way to obtain it.
	FetchJWT func(account Account, spiffeID string) (string, error)
	// Login exchanges a JWT-SVID with a Consul token, Valid tells if a token is still accepted and Logout destroys
	// one.
	Login  func(jwt string) (string, error)
	Valid  func(token string) (bool, error)
	Logout func(token string) error
	// Systemctl runs systemctl with the given arguments.
	Systemctl func(args ...string) error
	// Exec runs a process of the machine.
	Exec func(*Command) ([]byte, error)
	// Consul returns a client of the local agent using the given token, the one of a service.
	Consul func(token string) (Consul, error)
	// Sleep and Now allow tests to run without waiting.
	Sleep func(time.Duration)
	Now   func() time.Time
}

// SyncOptions tunes Sync.
type SyncOptions struct {
	// Services restricts the synchronization to the named definitions, all the installed ones when empty.
	Services []string
	// Renew obtains a new token for every service, even if the current one is still valid.
	Renew bool
	// NoRestart updates the files without reloading Consul nor restarting units.
	NoRestart bool
	// NoBlock queues the restarts without waiting for the units to start. It is needed when the synchronization runs
	// from the start of Consul itself, that the units to restart are ordered after.
	NoBlock bool
	// Setup runs the setup of the services as their package did when installed: the migrations of their
	// configuration, the generation of their service definitions and of what depends on the node, and the routing.
	// The other synchronizations only keep the credentials up to date.
	Setup bool
	// ForceRestart reloads Consul and restarts the units of the services even when nothing changed.
	ForceRestart bool
	// Wait is how long to wait for the SPIRE agent to issue an identity, that is available only some seconds after
	// the entries have been registered on the server, and for a service to be healthy before its routing.
	Wait time.Duration
	// Out receives one line per change, nothing when nil.
	Out io.Writer
}

const fetchInterval = 2 * time.Second

// outcome of the synchronization of one service.
type outcome struct {
	def         *Definition
	tokenChange bool // the token file has been created or replaced
	envChange   bool // a systemd drop-in has been written
	hclChange   bool // a rendered service definition has changed
	forced      bool // the setup asked to restart everything
	token       string
}

// Installed returns the definitions of the services installed on this node: the package has put its service
// definitions (or, for the services without one, its service-discover directory) on the disk, and the user of the
// service exists. Only these are synchronized.
func (e *Env) Installed(defs []Definition) []Definition {
	var installed []Definition

	for i := range defs {
		def := &defs[i]

		_, err := e.LookupUser(def.User)
		if err != nil || !e.packageInstalled(def) {
			continue
		}

		installed = append(installed, *def)
	}

	return installed
}

func (e *Env) packageInstalled(def *Definition) bool {
	if len(def.HCL) == 0 || def.Generated {
		return isDir(e.serviceDirectory(def))
	}

	return slices.ContainsFunc(def.HCL, func(name string) bool {
		return isFile(filepath.Join(e.ServicesDir, name))
	})
}

func (e *Env) serviceDirectory(def *Definition) string {
	return filepath.Join(e.CarbonioDir, def.Dir, "service-discover")
}

func (e *Env) tokenFile(def *Definition) string {
	return filepath.Join(e.serviceDirectory(def), "token")
}

// Sync brings the credentials of the services installed on this node up to date. For each one:
//   - a valid token in the token file is kept, otherwise a JWT-SVID is requested as the service user and exchanged for
//     a new token, written to the token file (the previous one is logged out);
//   - the service definitions of the package are rendered with the token, for Consul to load;
//   - the systemd drop-ins passing the token to the service are written.
//
// Then Consul is reloaded and the units depending on a changed file are restarted. Services failing to get a token
// do not stop the others: the failures are returned together.
func Sync(env *Env, defs []Definition, opts SyncOptions) error {
	selected, err := selectInstalled(env, defs, opts.Services)
	if err != nil {
		return err
	}

	log := func(format string, args ...any) {
		if opts.Out != nil {
			_, _ = fmt.Fprintf(opts.Out, format+"\n", args...)
		}
	}

	var (
		failures []error
		outcomes []outcome
	)

	deadline := env.Now().Add(opts.Wait)

	for i := range selected {
		res, syncErr := syncService(env, &selected[i], opts, deadline, log)
		res.forced = opts.ForceRestart

		if syncErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", selected[i].Name, syncErr))

			// A service that could not be set up is not routed.
			res.token = ""
		}

		// What changed before a failure still has to reach the units: the next run finds it up to date and would
		// not restart anything.
		outcomes = append(outcomes, res)
	}

	mirrored, err := mirrorPassThrough(env, defs, log)
	if err != nil {
		failures = append(failures, err)
	}

	removed, err := removeStale(env, defs, log)
	if err != nil {
		failures = append(failures, err)
	}

	if !opts.NoRestart {
		failures = append(failures, apply(env, outcomes, mirrored || len(removed) > 0, opts.NoBlock, log)...)

		if opts.Setup {
			failures = append(failures, finishSetup(env, outcomes, opts, log)...)
		}
	}

	return errors.Join(failures...)
}

func selectInstalled(env *Env, defs []Definition, names []string) ([]Definition, error) {
	installed := env.Installed(defs)
	if len(names) == 0 {
		return installed, nil
	}

	selected := make([]Definition, 0, len(names))

	for _, name := range names {
		def, found := Find(installed, name)
		if !found {
			if _, known := Find(defs, name); !known {
				return nil, fmt.Errorf("%w: %s", errUnknownService, name)
			}

			return nil, fmt.Errorf("%w: %s is not installed on this node", errUnknownService, name)
		}

		selected = append(selected, *def)
	}

	return selected, nil
}

func syncService(
	env *Env,
	def *Definition,
	opts SyncOptions,
	deadline time.Time,
	log func(string, ...any),
) (outcome, error) {
	res := outcome{def: def}

	account, err := env.LookupUser(def.User)
	if err != nil {
		return res, err
	}

	previous := readToken(env.tokenFile(def))

	token, changed, err := ensureToken(env, def, account, opts.Renew, deadline, log)
	if err != nil {
		return res, err
	}

	res.tokenChange = changed
	res.token = token

	if len(def.SubstituteFiles) > 0 {
		substituted, subErr := substitute(env, def, token, previous, log)
		if subErr != nil {
			return res, subErr
		}

		res.envChange = substituted
	}

	if opts.Setup {
		setupErr := prepareService(&setupContext{env: env, def: def, token: token, log: log, out: opts.Out})
		if setupErr != nil {
			return res, setupErr
		}
	}

	if !def.ConsumerOnly {
		res.hclChange, err = renderDefinitions(env, def, token, log)
		if err != nil {
			return res, err
		}
	}

	for _, tokenEnv := range def.TokenEnv {
		changedEnv, envErr := writeDropIn(env, tokenEnv, token, log)
		if envErr != nil {
			return res, envErr
		}

		res.envChange = res.envChange || changedEnv
	}

	return res, nil
}

// finishSetup runs the part of the setup of the services that needs them registered.
func finishSetup(env *Env, outcomes []outcome, opts SyncOptions, log func(string, ...any)) []error {
	var failures []error

	for _, res := range outcomes {
		if res.token == "" {
			continue
		}

		ctx := &setupContext{env: env, def: res.def, token: res.token, log: log, out: opts.Out}

		err := finishService(ctx, opts.Wait)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", res.def.Name, err))
		}
	}

	return failures
}

// ensureToken returns the token of the service, logging in when the token file has none or a stale one. The
// returned flag tells if the token file changed.
func ensureToken(
	env *Env,
	def *Definition,
	account Account,
	renew bool,
	deadline time.Time,
	log func(string, ...any),
) (string, bool, error) {
	path := env.tokenFile(def)

	current := readToken(path)
	if current != "" && !renew {
		valid, err := validToken(env, current, deadline)
		if err != nil {
			return "", false, fmt.Errorf("unable to validate the token: %w", err)
		}

		if valid {
			return current, false, nil
		}
	}

	jwt, err := fetchJWT(env, account, def.SpiffeID(env.TrustDomain), deadline)
	if err != nil {
		return "", false, err
	}

	token, err := env.Login(jwt)
	if err != nil {
		return "", false, fmt.Errorf("unable to log in Consul: %w", err)
	}

	err = writeFile(env, path, []byte(token), tokenFileMode, account)
	if err != nil {
		return "", false, err
	}

	log("%s: new token written to %s", def.Name, path)

	// The superseded token is not needed anymore: destroy it, an invalid one is already gone.
	if current != "" {
		valid, validErr := env.Valid(current)
		if validErr == nil && valid {
			logoutErr := env.Logout(current)
			if logoutErr != nil {
				log("%s: unable to log out the previous token: %v", def.Name, logoutErr)
			}
		}
	}

	return token, true, nil
}

// validToken asks Consul whether the token is valid, again until the deadline passes while Consul fails to answer:
// the sync run when the agent starts finds it without a leader for some seconds ("No cluster leader"), and a token
// that cannot be checked is not renewed.
func validToken(env *Env, token string, deadline time.Time) (bool, error) {
	for {
		valid, err := env.Valid(token)
		if err == nil || !env.Now().Before(deadline) {
			return valid, err
		}

		env.Sleep(fetchInterval)
	}
}

// fetchJWT asks for the JWT-SVID until it is issued or the deadline passes: the identity of a service becomes
// available on a node some seconds after its entry has been registered on the SPIRE server.
func fetchJWT(env *Env, account Account, spiffeID string, deadline time.Time) (string, error) {
	for {
		jwt, err := env.FetchJWT(account, spiffeID)
		if err == nil {
			return jwt, nil
		}

		if !env.Now().Before(deadline) {
			return "", err
		}

		env.Sleep(fetchInterval)
	}
}

func readToken(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(raw))
}

// renderDefinitions writes, for each service definition of the package present on the node, the copy with the token
// injected. It reports whether any of them changed.
func renderDefinitions(env *Env, def *Definition, token string, log func(string, ...any)) (bool, error) {
	consul, err := env.LookupUser(ConsulUser)
	if err != nil {
		return false, fmt.Errorf("unable to find the Consul user: %w", err)
	}

	changed := false

	for _, name := range def.HCL {
		src, readErr := os.ReadFile(filepath.Join(env.ServicesDir, name))
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}

		if readErr != nil {
			return changed, readErr
		}

		rendered, injectErr := InjectToken(src, name, token)
		if injectErr != nil {
			return changed, injectErr
		}

		target := filepath.Join(env.RenderedDir, name)

		same := bytes.Equal(readFileOrNil(target), rendered)
		if same {
			continue
		}

		err = os.MkdirAll(env.RenderedDir, renderedDirMode)
		if err != nil {
			return changed, err
		}

		err = env.Chown(env.RenderedDir, consul.UID, consul.GID)
		if err != nil {
			return changed, err
		}

		err = writeFile(env, target, rendered, renderedMode, consul)
		if err != nil {
			return changed, err
		}

		log("%s: rendered %s", def.Name, target)

		changed = true
	}

	return changed, nil
}

// writeDropIn writes the systemd drop-in passing the token to the service, as the package setup scripts did.
func writeDropIn(env *Env, tokenEnv TokenEnv, token string, log func(string, ...any)) (bool, error) {
	dir := filepath.Join(env.SystemdDir, tokenEnv.Unit+".d")
	path := filepath.Join(dir, "override.conf")
	content := fmt.Appendf(nil, "[Service]\nEnvironment=\"%s=%s\"\n", tokenEnv.Var, token)

	if bytes.Equal(readFileOrNil(path), content) {
		return false, nil
	}

	err := os.MkdirAll(dir, directoryMode)
	if err != nil {
		return false, err
	}

	err = writeFile(env, path, content, dropInMode, Account{UID: 0, GID: 0})
	if err != nil {
		return false, err
	}

	log("%s drop-in written to %s", tokenEnv.Unit, path)

	return true, nil
}

// mirrorPassThrough copies into RenderedDir, unchanged, the configuration files of ServicesDir that are not service
// definitions of a known service: the configuration of the agent and the definitions of unknown services. It reports
// whether any copy changed.
func mirrorPassThrough(env *Env, defs []Definition, log func(string, ...any)) (bool, error) {
	entries, err := os.ReadDir(env.ServicesDir)
	if err != nil {
		return false, fmt.Errorf("unable to read the configuration of the agent: %w", err)
	}

	consul, err := env.LookupUser(ConsulUser)
	if err != nil {
		return false, fmt.Errorf("unable to find the Consul user: %w", err)
	}

	changed := false

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isConfigFile(name) || knownHCL(defs, name) {
			continue
		}

		src, readErr := os.ReadFile(filepath.Join(env.ServicesDir, name))
		if readErr != nil {
			return changed, readErr
		}

		target := filepath.Join(env.RenderedDir, name)
		if bytes.Equal(readFileOrNil(target), src) {
			continue
		}

		err = os.MkdirAll(env.RenderedDir, renderedDirMode)
		if err != nil {
			return changed, err
		}

		err = env.Chown(env.RenderedDir, consul.UID, consul.GID)
		if err != nil {
			return changed, err
		}

		err = writeFile(env, target, src, renderedMode, consul)
		if err != nil {
			return changed, err
		}

		log("copied %s to %s", filepath.Join(env.ServicesDir, name), target)

		changed = true
	}

	return changed, nil
}

// removeStale deletes the files of RenderedDir that do not come from ServicesDir anymore: the definition of a removed
// package, or of a service that lost its token, must stop registering the service. It returns the removed names.
func removeStale(env *Env, defs []Definition, log func(string, ...any)) ([]string, error) {
	entries, err := os.ReadDir(env.RenderedDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	rendered := withToken(env, defs)

	var removed []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}

		// A copy stays while its source exists and, for a known service, while the service can be rendered.
		if isFile(filepath.Join(env.ServicesDir, name)) && (!knownHCL(defs, name) || knownHCL(rendered, name)) {
			continue
		}

		err = os.Remove(filepath.Join(env.RenderedDir, name))
		if err != nil {
			return removed, err
		}

		log("removed %s: the package definition is gone", filepath.Join(env.RenderedDir, name))

		removed = append(removed, name)
	}

	return removed, nil
}

func knownHCL(defs []Definition, name string) bool {
	return slices.ContainsFunc(defs, func(d Definition) bool { return slices.Contains(d.HCL, name) })
}

// apply reloads Consul when a service definition changed, and restarts the units reading something that changed:
// the sidecars read both the token and the definition, the other units only the token.
func apply(env *Env, outcomes []outcome, reload, noBlock bool, log func(string, ...any)) []error {
	var (
		failures []error
		restart  []string
		dropIns  bool
	)

	for _, res := range outcomes {
		reload = reload || res.hclChange || res.forced

		if res.tokenChange || res.hclChange || res.forced {
			restart = appendUnique(restart, res.def.Sidecars...)
		}

		if res.tokenChange || res.envChange || res.forced {
			restart = appendUnique(restart, res.def.Restart...)

			for _, tokenEnv := range res.def.TokenEnv {
				restart = appendUnique(restart, tokenEnv.Unit)
			}
		}

		dropIns = dropIns || res.envChange
	}

	if reload {
		failures = append(failures, reloadConsul(env, log)...)
	}

	if dropIns {
		err := env.Systemctl("daemon-reload")
		if err != nil {
			failures = append(failures, fmt.Errorf("systemctl daemon-reload: %w", err))
		}
	}

	return append(failures, restartUnits(env, restart, noBlock, log)...)
}

// reloadConsul makes the running Consul agent read its service definitions again. A stopped agent reads them at its
// start.
//
// The agent is signalled without checking first that the unit is active: the sync run by the unit itself
// (ExecStartPost) finds it activating, after the agent has read the definitions, and the ones it renders then (the
// services whose token was not issued yet) would not be loaded until the next change.
func reloadConsul(env *Env, log func(string, ...any)) []error {
	// consul reload is not used: it needs agent:write, that the token of a node does not have.
	err := env.Systemctl("kill", "--signal=SIGHUP", "--kill-whom=main", ConsulUnit)
	if err != nil {
		// No main process to signal: the agent is stopped and reads the definitions when it starts.
		if env.Systemctl("is-active", "--quiet", ConsulUnit) != nil {
			return nil
		}

		return []error{fmt.Errorf("unable to reload %s: %w", ConsulUnit, err)}
	}

	log("%s reloaded", ConsulUnit)

	return nil
}

func restartUnits(env *Env, units []string, noBlock bool, log func(string, ...any)) []error {
	var failures []error

	for _, unit := range units {
		// Units that are not installed are not a failure: the package may not ship them.
		if env.Systemctl("cat", unit) != nil {
			continue
		}

		args := []string{"restart", unit}
		if noBlock {
			args = []string{"restart", "--no-block", unit}
		}

		err := env.Systemctl(args...)
		if err != nil {
			failures = append(failures, fmt.Errorf("unable to restart %s: %w", unit, err))

			continue
		}

		log("%s restarted", unit)
	}

	return failures
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}

	return list
}

// writeFile atomically replaces path with content, owned by account.
func writeFile(env *Env, path string, content []byte, mode os.FileMode, account Account) error {
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, directoryMode)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".mesh-*")
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	_, err = tmp.Write(content)
	closeErr := tmp.Close()

	err = errors.Join(err, closeErr)
	if err != nil {
		return err
	}

	err = os.Chmod(tmp.Name(), mode)
	if err != nil {
		return err
	}

	err = env.Chown(tmp.Name(), account.UID, account.GID)
	if err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}

func readFileOrNil(path string) []byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	return raw
}

func isFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}
