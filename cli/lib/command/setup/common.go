package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"html/template"
	"io/ioutil"
	"net"
	"os"
	"strings"
)

type ConsulRole = string

const (
	ConsulCA                   = "consul-agent-ca.pem"
	ConsulCAKey                = "consul-agent-ca-key.pem"
	ConsulServerCertificate    = "dc1-server-consul-0.pem"
	ConsulServerCertificateKey = "dc1-server-consul-0-key.pem"
	ConsulAgentCertificate     = "dc1-client-consul-0.pem"
	ConsulAgentCertificateKey  = "dc1-client-consul-0-key.pem"
	ConsulAclBootstrap         = "consul-acl-secret.json"
	GossipKey                  = "gossip-key"
	ConsulHttpToken            = "CONSUL_HTTP_TOKEN"
	ConsulBin                  = "/usr/bin/consul"
	AclPolicyTemplateText      = `{
   "node":{
      "{{ .ZimbraHostname }}":{
         "policy":"write"
      }
   },
   "node_prefix":{
      "":{
         "policy":"read"
      }
   },
   "service_prefix":{
      "":{
         "policy":"write"
      }
   }
}`
	Agent  ConsulRole = "agent"
	Server ConsulRole = "server"
)

type NetworkInterfaces interface {
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LookupIP(s string) ([]net.IP, error)
}

type ACLPolicies struct {
	ID   string `json:"ID"`
	Name string `json:"Name"`
}

type ACLTokenCreation struct {
	AccessorID  string        `json:"AccessorID"`
	CreateIndex int64         `json:"CreateIndex"`
	CreateTime  string        `json:"CreateTime"`
	Description string        `json:"Description"`
	Hash        string        `json:"Hash"`
	Local       bool          `json:"Local"`
	ModifyIndex int64         `json:"ModifyIndex"`
	Policies    []ACLPolicies `json:"Policies"`
	SecretID    string        `json:"SecretID"`
}

// OpenClusterCredential checks that the given path, s.ClusterCredential exists and it is readable
func OpenClusterCredential(clusterCredential string) (*os.File, error) {
	clusterCredentialFile, err := os.Open(clusterCredential)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New(fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				clusterCredential,
			))
		} else {
			return nil, err
		}
	}
	return clusterCredentialFile, nil
}

func AddrsToSingleString(addrs *[]net.Addr, sep string) string {
	strAddrs := make([]string, len(*addrs))
	for i, a := range *addrs {
		if a.String() != "" {
			strAddrs[i] = a.String()
		}
	}
	return strings.Join(strAddrs, sep)
}

func CheckValidBindingAddress(resolver NetworkInterfaces, networks []net.Interface, bindingAddress string) error {
	isBindingAddressValid := false
	for _, n := range networks {
		addrs, _ := resolver.AddrResolver(n)
		for _, a := range addrs {
			if bindingAddress == a.String() || bindingAddress == strings.Split(a.String(), "/")[0] {
				isBindingAddressValid = true
			}
		}
	}
	if !isBindingAddressValid {
		return errors.New("invalid binding address selected")
	}
	return nil
}

// NonLoopbackInterfaces returns all the network interfaces but the loopback one
func NonLoopbackInterfaces(d NetworkInterfaces) ([]net.Interface, error) {
	networks, err := d.NetInterfaces()
	if err != nil {
		return nil, err
	}

	for i, n := range networks {
		if strings.ToLower(n.Name) == "lo" {
			networks[i] = networks[len(networks)-1]
			networks = networks[:len(networks)-1]
		}
	}
	return networks, nil
}

// RetrieveZimbraHostname returns the zimbra.LocalConfigServerHostname value, but only after checking that the
// LDAP server is up
func RetrieveZimbraHostname(localConfig zimbra.LocalConfig, ldapHandler zimbra.LdapHandler) (string, error) {
	err := ldapHandler.CheckServerAvailability(true)
	if err != nil {
		return "", errors.New("unable to connect to ldap: " + err.Error())
	}
	return localConfig.Value(zimbra.LocalConfigServerHostname), nil
}

func AddServiceInLDAP(ldap zimbra.LdapHandler, zimbraHostname string) error {
	err := ldap.AddService(zimbraHostname, zimbra.ServiceDiscoverServiceName)
	if err != nil {
		return errors.New("cannot add service in ldap: " + err.Error())
	}
	return nil
}

// SaveBindAddressConfiguration adds the bindAddress to the Consul configuration file
func SaveBindAddressConfiguration(mutableConfig string, bindAddress string) error {
	if strings.Contains(bindAddress, "/") {
		bindAddress = strings.Split(bindAddress,"/")[0]
	}
	mutableConsulConfig := command.MutableConsulConfig{BindAddress: bindAddress}
	bs, err := json.MarshalIndent(mutableConsulConfig, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(mutableConfig, bs, os.FileMode(0644))
}

// ConsulNodeName allows you to retrieve the Consul node name based on the hostname and its role
func ConsulNodeName(prefix ConsulRole, hostname string) string {
	return strings.Replace(fmt.Sprintf("%s-%s", prefix, hostname), ".", "-", -1)
}

func CreateACLToken(
	commandCreator func(name string, args ...string) exec.Cmd,
	prefix ConsulRole,
	zimbraHostname string,
	rootToken string,
) (string, error) {
	if err := os.Setenv(ConsulHttpToken, rootToken); err != nil {
		return "", errors.WithMessage(err, "unable to set correct env variable before starting ACL token creation")
	}
	defer os.Unsetenv(ConsulHttpToken)
	agentPolicyName := ConsulNodeName(prefix, zimbraHostname)
	templateRender := struct {
		ZimbraHostname string
	}{ZimbraHostname: agentPolicyName}
	aclPolicyTemplate := template.Must(template.New("agent-config").Parse(AclPolicyTemplateText))
	aclPolicyRenderBuffer := bytes.Buffer{}
	if err := aclPolicyTemplate.Execute(&aclPolicyRenderBuffer, templateRender); err != nil {
		return "", err
	}
	aclPolicyBs, err := ioutil.ReadAll(&aclPolicyRenderBuffer)
	if err != nil {
		return "", err
	}

	policyCreationCmd := commandCreator(ConsulBin,
		"acl",
		"policy",
		"create",
		"-name",
		agentPolicyName,
		"-rules",
		string(aclPolicyBs),
	)
	_, _ = policyCreationCmd.Output()

	// TODO: this will force token re-creation, that could be avoided. This is only a tidying up.
	tokenCreationCmd := commandCreator(ConsulBin,
		"acl",
		"token",
		"create",
		"-policy-name",
		agentPolicyName,
		"-format",
		"json",
	)
	tokenCmdResp, err := tokenCreationCmd.Output()
	if err != nil {
		return "", exec.ErrorFromStderr(err, "unable to create ACL token for policy "+agentPolicyName)
	}
	token := ACLTokenCreation{}
	if err := json.Unmarshal(tokenCmdResp, &token); err != nil {
		return "", errors.WithMessage(err, "unable to decode response from consul agent")
	}

	return token.SecretID, nil
}

func SetACLToken(commandCreator func(name string, args ...string) exec.Cmd, token string, rootToken string) error {
	if err := os.Setenv(ConsulHttpToken, rootToken); err != nil {
		return errors.WithMessage(err, "unable to set correct env variable before starting ACL token creation")
	}
	defer os.Unsetenv(ConsulHttpToken)
	setupAclCmd := commandCreator(ConsulBin,
		"acl",
		"set-agent-token",
		"default",
		token,
	)
	if _, err := setupAclCmd.Output(); err != nil {
		return exec.ErrorFromStderr(err, "unable to set agent token")
	}

	return nil
}

func CheckHostnameAddress(d NetworkInterfaces, hostname string) error {
	addrs, err := d.LookupIP(hostname)
	if err != nil {
		return errors.WithMessagef(err,"cannot resolve hostname '%s'", hostname)
	}
	if len(addrs) == 0 {
		return errors.Errorf("cannot resolve hostname '%s'", hostname)
	}
	for _, addr := range addrs {
		if addr.IsLoopback(){
			return errors.New(fmt.Sprintf("hostname '%s' is resolving with loopback address, should resolve with LAN address", hostname))
		}
	}
	return nil
}