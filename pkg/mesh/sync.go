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
	// Wait is how long to wait for the SPIRE agent to issue an identity, that is available only some seconds after
	// the entries have been registered on the server.
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
	if len(def.HCL) == 0 {
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
		if syncErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", selected[i].Name, syncErr))
		}

		// What changed before a failure still has to reach the units: the next run finds it up to date and would
		// not restart anything.
		outcomes = append(outcomes, res)
	}

	removed, err := removeStale(env, withToken(env, defs), log)
	if err != nil {
		failures = append(failures, err)
	}

	if !opts.NoRestart {
		failures = append(failures, apply(env, outcomes, removed, opts.NoBlock, log)...)
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

	token, changed, err := ensureToken(env, def, account, opts.Renew, deadline, log)
	if err != nil {
		return res, err
	}

	res.tokenChange = changed

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
		valid, err := env.Valid(current)
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

// removeStale deletes the rendered definitions that do not have a package definition anymore, so that a removed
// package stops registering its service. It returns the removed names.
func removeStale(env *Env, defs []Definition, log func(string, ...any)) ([]string, error) {
	entries, err := os.ReadDir(env.RenderedDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var removed []string

	for _, entry := range entries {
		packaged := isFile(filepath.Join(env.ServicesDir, entry.Name())) && knownHCL(defs, entry.Name())
		if entry.IsDir() || packaged {
			continue
		}

		err = os.Remove(filepath.Join(env.RenderedDir, entry.Name()))
		if err != nil {
			return removed, err
		}

		log("removed %s: the package definition is gone", filepath.Join(env.RenderedDir, entry.Name()))

		removed = append(removed, entry.Name())
	}

	return removed, nil
}

func knownHCL(defs []Definition, name string) bool {
	return slices.ContainsFunc(defs, func(d Definition) bool { return slices.Contains(d.HCL, name) })
}

// apply reloads Consul when a service definition changed, and restarts the units reading something that changed:
// the sidecars read both the token and the definition, the other units only the token.
func apply(env *Env, outcomes []outcome, removed []string, noBlock bool, log func(string, ...any)) []error {
	var (
		failures []error
		reload   = len(removed) > 0
		restart  []string
		dropIns  bool
	)

	for _, res := range outcomes {
		reload = reload || res.hclChange

		if res.tokenChange || res.hclChange {
			restart = appendUnique(restart, res.def.Sidecars...)
		}

		if res.tokenChange || res.envChange {
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
func reloadConsul(env *Env, log func(string, ...any)) []error {
	if env.Systemctl("is-active", "--quiet", ConsulUnit) != nil {
		return nil
	}

	// consul reload is not used: it needs agent:write, that the token of a node does not have.
	err := env.Systemctl("kill", "--signal=SIGHUP", "--kill-whom=main", ConsulUnit)
	if err != nil {
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
