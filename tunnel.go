package dbman

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	promptNumRetries = 3
)

type Tunnel struct {
	config     ssh.ClientConfig
	tunnelHost string
	remoteHost string

	localConn net.Listener
	client    *ssh.Client

	connections []io.Closer
	mu          sync.Mutex
}

func NewTunnel(prompter ssh.KeyboardInteractiveChallenge, tunnel *SSHTunnel, host string, port int) (*Tunnel, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not locate known_hosts: %w", err)
	}

	var hostKeyCB ssh.HostKeyCallback
	switch {
	case tunnel.HostPublicKeyFile != "":
		buf, err := ioutil.ReadFile(tunnel.HostPublicKeyFile)
		if err != nil {
			return nil, fmt.Errorf("could not read expected host public key: %w", err)
		}
		hostKey, err := ssh.ParsePublicKey(buf)
		if err != nil {
			return nil, fmt.Errorf("invalid host public key: %w", err)
		}
		hostKeyCB = ssh.FixedHostKey(hostKey)

	case !tunnel.DisableVerifyKnownHost:
		hostKeyCB = knownHostsCallback(filepath.Join(homedir, ".ssh/known_hosts"))

	default:
		hostKeyCB = ssh.InsecureIgnoreHostKey()
	}

	var auth ssh.AuthMethod
	switch tunnel.AuthMethod {
	case PasswordAuth:
		if tunnel.Password != "" {
			auth = ssh.Password(tunnel.Password)
		} else {
			auth = ssh.RetryableAuthMethod(ssh.KeyboardInteractive(prompter), promptNumRetries)
		}

	case PublicKeyAuth:
		privateKeyFile := strings.ReplaceAll(os.ExpandEnv(tunnel.PrivateKeyFile), "~", homedir)
		buf, err := ioutil.ReadFile(privateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("could not read private key file: %w", err)
		}

		auth = ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			signer, err := ssh.ParsePrivateKey(buf)
			if err != nil {
				needPass := new(ssh.PassphraseMissingError)
				if !errors.As(err, &needPass) {
					return nil, fmt.Errorf("could not parse private key: %w", err)
				}

				if tunnel.PrivateKeyPassphrase != "" {
					signer, err = ssh.ParsePrivateKeyWithPassphrase(buf, []byte(tunnel.PrivateKeyPassphrase))
				} else {
					for i := 0; i < promptNumRetries; i++ {
						var answers []string
						answers, err = prompter(tunnel.Host, "private key is encrypted", []string{"private key passphrase: "}, []bool{false})
						if err != nil {
							log.Print(err)
							continue
						}

						signer, err = ssh.ParsePrivateKeyWithPassphrase(buf, []byte(answers[0]))
						if err == nil {
							break
						}
					}
				}
				if err != nil {
					return nil, fmt.Errorf("could not decrypt private key: %w", err)
				}
			}
			return []ssh.Signer{signer}, nil
		})

	case AgentAuth:
		socket := os.Getenv("SSH_AUTH_SOCK")
		agentConn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, fmt.Errorf("could not open SSH_AUTH_SOCK: %w", err)
		}
		agentClient := agent.NewClient(agentConn)

		auth = ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			signers, err := agentClient.Signers()
			if err != nil {
				log.Println("error getting signers from ssh agent:", err)
				return nil, err
			}
			return signers, nil
		})
	}

	localConn, err := net.Listen("tcp", "localhost:0") // 0 for port picks a random available port
	if err != nil {
		return nil, fmt.Errorf("could not open local port: %w", err)
	}

	t := &Tunnel{
		config: ssh.ClientConfig{
			User:            tunnel.User,
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: hostKeyCB,
			BannerCallback:  ssh.BannerDisplayStderr(),
			Timeout:         time.Duration(tunnel.ConnectTimeoutSec) * time.Second,
		},
		tunnelHost: tunnel.Host + ":" + strconv.Itoa(tunnel.Port),
		remoteHost: host + ":" + strconv.Itoa(port),
		localConn:  localConn,
	}

	t.client, err = ssh.Dial("tcp", t.tunnelHost, &t.config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to tunnel: %w", err)
	}

	go func() {
		for {
			conn, err := localConn.Accept()
			if err != nil {
				// TODO hopefully a better way to identify closed errors
				if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
					return
				}
				log.Print("error accepting tunnel connection:", err)
				continue
			}

			t.mu.Lock()
			t.connections = append(t.connections, conn)
			t.mu.Unlock()
			go t.forward(conn)
		}
	}()

	return t, nil
}

func (t *Tunnel) forward(localConn net.Conn) {
	remoteConn, err := t.client.Dial("tcp", t.remoteHost)
	if err != nil {
		log.Print("could not establish remote connection to database:", err)
		return
	}

	go logCopy(localConn, remoteConn)
	go logCopy(remoteConn, localConn)
}

func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, cl := range t.connections {
		cl.Close()
	}
	return t.localConn.Close()
}
