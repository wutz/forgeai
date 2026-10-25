package sshgateway

// Keys of the Secret that holds the sshd keys of an SSH Service: the host key
// the gateway pins, and the platform key sshd accepts.
const (
	SSHHostKeyKey       = "ssh_host_ed25519_key"
	SSHHostKeyPubKey    = "ssh_host_ed25519_key.pub"
	SSHAuthorizedKeyKey = "authorized_keys"
)

// Paths of the sshd keys in the sshd sidecar of a sandbox. The Secret is
// mounted at SSHDKeysDir; the entrypoint of the sidecar copies the keys to
// SSHDRunDir, which it owns and sshd's StrictModes accepts, and sshd reads them
// there.
const (
	SSHDKeysDir = "/etc/forgeai-sshd"
	SSHDRunDir  = "/run/forgeai-sshd"

	SSHDHostKeyPath    = SSHDRunDir + "/" + SSHHostKeyKey
	SSHDAuthorizedKeys = SSHDRunDir + "/" + SSHAuthorizedKeyKey
)

// SSHDEntrypoint is the command of the sshd sidecar: it stages the keys at
// SSHDRunDir and execs sshd with the options below.
const SSHDEntrypoint = "/usr/local/bin/forgeai-sshd"

// SSHDArgs are the sshd arguments of a sandbox sidecar. They keep the default
// configuration of the image, and override it so that the only way in is a
// public key signed by the platform identity: no passwords, no keyboard
// interaction, no forwarding, one login account.
func SSHDArgs() []string {
	options := []string{
		"HostKey=" + SSHDHostKeyPath,
		"AuthorizedKeysFile=" + SSHDAuthorizedKeys,
		"AuthenticationMethods=publickey",
		"PubkeyAuthentication=yes",
		"PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no",
		"PermitEmptyPasswords=no",
		"PermitRootLogin=no",
		"PermitUserEnvironment=no",
		"UsePAM=no",
		"AllowUsers=" + SandboxUser,
		"AllowAgentForwarding=no",
		"AllowTcpForwarding=no",
		"GatewayPorts=no",
		"PermitTunnel=no",
		"X11Forwarding=no",
		"StrictModes=yes",
		"LogLevel=VERBOSE",
	}
	args := make([]string, 0, 2+2*len(options))
	args = append(args, "-D", "-e")
	for _, o := range options {
		args = append(args, "-o", o)
	}
	return args
}
