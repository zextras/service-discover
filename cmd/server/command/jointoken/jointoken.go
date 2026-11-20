// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package jointoken implements the join-token command of the server CLI, that lets a new node join the cluster
// through SPIRE instead of the cluster credentials.
package jointoken

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/hashicorp/consul/api/v2"
	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	rootUID            = 0
	consulPeersTimeout = 10 * time.Second
	rootSelector       = "unix:uid:0"
)

var errInvalidHost = errors.New("invalid host: expected a fully qualified host name")

// hostPattern accepts the host names that can safely be part of a SPIFFE ID path.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

type dependencies interface {
	CreateCommand(name string, args ...string) exec.Cmd
	GetuidSyscall() int
	// ServerHostname returns the host name of this server as reachable by the other nodes.
	ServerHostname() (string, error)
	// ConsulServers returns the RPC addresses of the Consul servers of the cluster.
	ConsulServers() ([]string, error)
}

type realDependencies struct {
	localConfigPath string
}

func (r realDependencies) CreateCommand(name string, args ...string) exec.Cmd {
	return exec.Command(name, args...)
}

func (r realDependencies) GetuidSyscall() int {
	return os.Getuid()
}

func (r realDependencies) ServerHostname() (string, error) {
	localConfig, err := carbonio.LoadLocalConfig(r.localConfigPath)
	if err != nil {
		return "", err
	}

	return localConfig.Value(carbonio.LocalConfigServerHostname), nil
}

// ConsulServers asks the local agent for the Raft peers, an unauthenticated endpoint.
func (r realDependencies) ConsulServers() ([]string, error) {
	cfg := api.DefaultNonPooledConfig()
	cfg.HttpClient = &http.Client{Transport: cfg.Transport, Timeout: consulPeersTimeout}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return client.Status().Peers()
}

// JoinToken generates the bundle a node needs to join the cluster through SPIRE. Run it on a server, as root.
type JoinToken struct {
	Host string        `required:"" help:"Fully qualified host name of the node that will join the cluster"`
	TTL  time.Duration `default:"10m" help:"Validity of the join token, the node has to join before it expires"`

	consulHome string       `kong:"-"`
	deps       dependencies `kong:"-"`
	writer     io.Writer    `kong:"-"`
}

// New returns a JoinToken command reading the real system state and writing on writer.
func New(writer io.Writer) JoinToken {
	return JoinToken{
		consulHome: config.ConsulHome,
		deps:       realDependencies{localConfigPath: config.LocalConfigPath},
		writer:     writer,
	}
}

type output struct {
	JoinBundle string `json:"join_bundle"`
	NodeName   string `json:"node_name"`
	SpiffeID   string `json:"spiffe_id"`
}

func (o *output) PlainRender() (string, error) {
	return o.JoinBundle + "\n", nil
}

func (o *output) JSONRender() (string, error) {
	return formatter.DefaultJSONRender(o)
}

// Run implements the command.
func (j *JoinToken) Run(commonFlags *command.GlobalCommonFlags) error {
	out, err := j.generate()
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(j.writer, render)

	return err
}

func (j *JoinToken) generate() (*output, error) {
	if j.deps.GetuidSyscall() != rootUID {
		return nil, errors.New("this command must be executed as root")
	}

	if !hostPattern.MatchString(j.Host) {
		return nil, errors.WithMessagef(errInvalidHost, "'%s'", j.Host)
	}

	serverHostname, err := j.deps.ServerHostname()
	if err != nil {
		return nil, err
	}

	caPEM, err := os.ReadFile(filepath.Join(j.consulHome, command.ConsulCA)) // #nosec
	if err != nil {
		return nil, errors.WithMessage(err, "unable to read the Consul CA")
	}

	servers, err := j.deps.ConsulServers()
	if err != nil {
		return nil, errors.WithMessage(err, "unable to get the Consul servers from the local agent")
	}

	cli := spire.NewServerCLI(j.deps.CreateCommand)
	nodeName := command.ConsulNodeName(command.Agent, j.Host)

	// The entry allows the node, once attested with the token, to get the JWT-SVID to join Consul. It does not
	// depend on the token, so it is created first and a failure does not burn a token.
	_, err = cli.EnsureEntry(spire.NodeID(j.Host), spire.ConsulID(nodeName), rootSelector)
	if err != nil {
		return nil, err
	}

	// The services of the node get their identity from the SPIRE agent of the node, whichever package is installed
	// there now or later: the entries are the ones of every service defined by service-discover.
	defs, err := mesh.Load()
	if err != nil {
		return nil, err
	}

	_, err = mesh.EnsureEntries(cli, defs, spire.TrustDomain, j.Host)
	if err != nil {
		return nil, err
	}

	token, err := cli.GenerateJoinToken(spire.NodeID(j.Host), j.TTL)
	if err != nil {
		return nil, err
	}

	trustBundle, err := cli.BundlePEM()
	if err != nil {
		return nil, err
	}

	bundle := &spire.JoinBundle{
		Version:       spire.BundleVersion,
		TrustDomain:   spire.TrustDomain,
		SpireServer:   fmt.Sprintf("%s:%d", serverHostname, spire.ServerPort),
		SpireBundle:   trustBundle,
		JoinToken:     token,
		ConsulCA:      string(caPEM),
		ConsulServers: servers,
		NodeName:      nodeName,
	}

	encoded, err := bundle.Encode()
	if err != nil {
		return nil, err
	}

	return &output{JoinBundle: encoded, NodeName: nodeName, SpiffeID: spire.NodeID(j.Host)}, nil
}
