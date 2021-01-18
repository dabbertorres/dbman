package dbman

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func logCopy(dst io.Writer, src io.Reader) {
	if _, err := io.Copy(dst, src); err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			// TODO hopefully a better way to identify closed errors
			if opErr.Temporary() {
				log.Print("io.Copy error:", err)
			} else {
				return
			}
		}
	}
}

func PasswordPrompt(terminal *term.Terminal) func(user, instruction string, questions []string, echos []bool) ([]string, error) {
	return func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		terminal.SetBracketedPasteMode(true)
		defer terminal.SetBracketedPasteMode(false)

		if user != "" || instruction != "" {
			fmt.Fprintf(terminal, "%s: %s\n", user, instruction)
		}
		answers := make([]string, len(questions))

		for i := 0; i < len(questions); i++ {
			var err error
			if echos[i] {
				terminal.SetPrompt(questions[i])
				answers[i], err = terminal.ReadLine()
			} else {
				answers[i], err = terminal.ReadPassword(questions[i])
			}

			if err != nil && err != term.ErrPasteIndicator {
				return nil, err
			}
		}

		return answers, nil
	}
}

func knownHostsCallback(path string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// known hosts file doesn't always include a port
		host, _, err := net.SplitHostPort(hostname)
		if err != nil {
			return err
		}

		remoteKeyRaw := bytes.TrimSpace(key.Marshal())

		buf, err := ioutil.ReadFile(path)
		if err != nil {
			log.Fatal("could not read known_hosts:", err)
		}

		for {
			var (
				marker   string
				hosts    []string
				knownKey ssh.PublicKey
				err      error
			)
			marker, hosts, knownKey, _, buf, err = ssh.ParseKnownHosts(buf)
			if err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			if marker == "revoked" {
				continue
			}

			if stringsContains(hosts, host) || !stringsContains(hosts, remote.String()) {
				if !bytes.Equal(remoteKeyRaw, bytes.TrimSpace(knownKey.Marshal())) {
					ioutil.WriteFile("remote-key", remoteKeyRaw, 0644)
					ioutil.WriteFile("known-key", knownKey.Marshal(), 0644)
					return fmt.Errorf("remote public key from '%s' does not match known public key", host)
				}

				// got a match!
				return nil
			}
		}

		return fmt.Errorf("'%s' is an unknown host", hostname)
	}
}

func stringsContains(list []string, str string) bool {
	for _, s := range list {
		if s == str {
			return true
		}
	}
	return false
}
