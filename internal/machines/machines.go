// Package machines defines the ~/.obey/machines.yaml registry: the shared,
// daemon-free description of the ssh/Tailscale-reachable machines whose
// campaigns camp can list and switch into.
//
// The file is the cross-tool contract. Its schema mirrors the festival app's
// RemoteMachine (projects/festival-app/src-tauri/src/state.rs) field-for-field
// so the app can adopt the same file with a serde rename rather than a
// redesign. Fields camp alone needs do not belong here; add them and the shared
// shape has drifted.
package machines

// currentVersion is the schema version stamped into new machines.yaml files.
const currentVersion = 1

// LocalMachineID is the reserved id for the current machine. It is implicit and
// always available to callers, and is never written to machines.yaml. Mirrors
// the app's LOCAL_MACHINE_ID (festival-app src-tauri/src/remote/mod.rs).
const LocalMachineID = "local"

// The permitted auth_method values, mirroring the app (state.rs auth_method and
// the SSH_PASSWORD_AUTH const family at remote/connection.rs).
const (
	AuthTailscaleSSH = "tailscale-ssh"
	AuthSSHAgent     = "ssh-agent"
	AuthSSHPassword  = "ssh-password"
)

// File is the on-disk shape of ~/.obey/machines.yaml.
type File struct {
	Version  int       `yaml:"version"`
	Machines []Machine `yaml:"machines"`
}

// Machine mirrors the festival-app RemoteMachine field-for-field so the app can
// adopt this file with a serde rename, not a redesign. The reserved id "local"
// (LocalMachineID) is never persisted as a Machine.
type Machine struct {
	ID           string `yaml:"id"`
	Label        string `yaml:"label"`
	Host         string `yaml:"host"`
	AuthMethod   string `yaml:"auth_method"`
	SSHUser      string `yaml:"ssh_user,omitempty"`
	IdentityFile string `yaml:"identity_file,omitempty"`
}
