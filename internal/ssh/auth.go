package sshx

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// KeyAuth loads a private key from path and returns an ssh.AuthMethod. If
// path is empty it returns an unconfigured agent-based auth (nil is NOT
// returned so callers can always append). A passphrase may be supplied for
// encrypted keys.
func KeyAuth(path, passphrase string) (ssh.AuthMethod, error) {
	if path == "" {
		return nil, fmt.Errorf("key path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", path, err)
	}
	var signer ssh.Signer
	signer, err = ssh.ParsePrivateKey(data)
	if err != nil {
		if passphrase == "" {
			return nil, fmt.Errorf("parse key %s: %w", path, err)
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("parse key %s: %w", path, err)
		}
	}
	return ssh.PublicKeys(signer), nil
}

// PasswordAuth returns an ssh.AuthMethod using the given password.
func PasswordAuth(password string) ssh.AuthMethod {
	return ssh.Password(password)
}

// AgentAuth finds an authentication method backed by the ssh-agent socket,
// so keys held by the user's agent are used without extra configuration.
func AgentAuth() (ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("connect ssh-agent: %w", err)
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers), nil
}

// CredentialAuth resolves the auth methods for a stored credential: a
// password credential uses Password; otherwise it tries the ssh-agent first
// and then the configured private key.
func CredentialAuth(c *Credential) ([]ssh.AuthMethod, error) {
	if c == nil {
		return nil, fmt.Errorf("empty credential")
	}
	if c.AuthType == "password" {
		if c.Password == "" {
			return nil, fmt.Errorf("credential %q has no password", c.Name)
		}
		return []ssh.AuthMethod{ssh.Password(c.Password)}, nil
	}
	var methods []ssh.AuthMethod
	if m, err := AgentAuth(); err == nil {
		methods = append(methods, m)
	}
	if c.KeyPath != "" {
		if c.AuthType != "" && c.AuthType != "key" {
			return nil, fmt.Errorf("credential %q: unknown auth type %q", c.Name, c.AuthType)
		}
		m, err := KeyAuth(c.KeyPath, c.Passphrase)
		if err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("credential %q: no authentication configured", c.Name)
	}
	return methods, nil
}
