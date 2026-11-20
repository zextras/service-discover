// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zextras/service-discover/pkg/spire"
)

var errOffline = errors.New("not available offline")

const (
	fetchTimeout    = 15 * time.Second
	systemctlBinary = "systemctl"
)

// NewEnv returns the Env reading and changing the real machine, logging in the local Consul agent (CONSUL_HTTP_ADDR
// applies).
func NewEnv(trustDomain string) (*Env, error) {
	consul, err := newConsulLogin(spire.AuthMethodName)
	if err != nil {
		return nil, err
	}

	return &Env{
		ServicesDir: ServicesDir,
		RenderedDir: RenderedDir,
		CarbonioDir: CarbonioDir,
		SystemdDir:  SystemdDir,
		TrustDomain: trustDomain,
		LookupUser:  lookupUser,
		Chown:       os.Chown,
		FetchJWT:    fetchJWTAs,
		Login:       consul.Login,
		Valid:       consul.Valid,
		Logout:      consul.Logout,
		Systemctl:   systemctl,
		Sleep:       time.Sleep,
		Now:         time.Now,
		CertDir:     CertDir,
		Exec:        execCommand,
		Consul:      newConsulService,
	}, nil
}

// NewOfflineEnv returns the Env for the operations not involving Consul nor SPIRE (the preparation of the
// configuration at the start of the agent): any attempt to use them is an error.
func NewOfflineEnv(trustDomain string) *Env {
	return &Env{
		ServicesDir: ServicesDir,
		RenderedDir: RenderedDir,
		CarbonioDir: CarbonioDir,
		SystemdDir:  SystemdDir,
		TrustDomain: trustDomain,
		LookupUser:  lookupUser,
		Chown:       os.Chown,
		FetchJWT:    func(Account, string) (string, error) { return "", errOffline },
		Login:       func(string) (string, error) { return "", errOffline },
		Valid:       func(string) (bool, error) { return false, errOffline },
		Logout:      func(string) error { return errOffline },
		Systemctl:   systemctl,
		Sleep:       time.Sleep,
		Now:         time.Now,
		CertDir:     CertDir,
		Exec:        execCommand,
		Consul:      func(string) (Consul, error) { return nil, errOffline },
	}
}

func lookupUser(name string) (Account, error) {
	found, err := user.Lookup(name)
	if err != nil {
		if _, unknown := errors.AsType[user.UnknownUserError](err); unknown {
			return Account{}, fmt.Errorf("user %s: %w", name, fs.ErrNotExist)
		}

		return Account{}, err
	}

	uid, err := strconv.Atoi(found.Uid)
	if err != nil {
		return Account{}, err
	}

	gid, err := strconv.Atoi(found.Gid)
	if err != nil {
		return Account{}, err
	}

	return Account{UID: uid, GID: gid}, nil
}

// fetchJWTAs runs the SPIRE agent client with the identity of account: the registration entry of a service matches
// the processes of its user, so the identity cannot be requested by root on its behalf and root never impersonates
// another user beyond starting a process as it.
func fetchJWTAs(account Account, spiffeID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	//nolint:gosec // fixed binary, the SPIFFE ID comes from the embedded definitions
	cmd := exec.CommandContext(ctx, spire.AgentBin, "api", "fetch", "jwt",
		"-audience", spire.AudienceLogin, "-spiffeID", spiffeID, "-socketPath", spire.AgentSocket)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		//nolint:gosec // ids of /etc/passwd
		Credential: &syscall.Credential{Uid: uint32(account.UID), Gid: uint32(account.GID)},
	}

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("unable to fetch a JWT-SVID for %s: %w: %s", spiffeID, err, strings.TrimSpace(stderr.String()))
	}

	return spire.ParseJWTSVID(string(out))
}

func systemctl(args ...string) error {
	out, err := exec.Command(systemctlBinary, args...).CombinedOutput() //nolint:noctx // fixed binary
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}
