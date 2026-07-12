package machines

import "testing"

func TestSchemaContract(t *testing.T) {
	if LocalMachineID != "local" {
		t.Fatalf("LocalMachineID = %q, want %q", LocalMachineID, "local")
	}
	for name, got := range map[string]string{
		"tailscale": AuthTailscaleSSH,
		"agent":     AuthSSHAgent,
		"password":  AuthSSHPassword,
	} {
		if got == "" {
			t.Fatalf("auth method %q constant is empty", name)
		}
	}

	m := Machine{
		ID:           "devbox",
		Label:        "Dev Box",
		Host:         "devbox.tailnet.ts.net",
		AuthMethod:   AuthTailscaleSSH,
		SSHUser:      "lance",
		IdentityFile: "~/.ssh/id_ed25519",
	}
	f := File{Version: currentVersion, Machines: []Machine{m}}
	if f.Version != currentVersion || len(f.Machines) != 1 || f.Machines[0].ID != "devbox" {
		t.Fatalf("File construction failed: %+v", f)
	}
}

func TestMachinesPathIsObeySibling(t *testing.T) {
	t.Setenv("CAMP_MACHINES_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/mm-xdg-unit-only")
	if got, want := MachinesPath(), "/tmp/mm-xdg-unit-only/obey/machines.yaml"; got != want {
		t.Fatalf("MachinesPath() under XDG = %q, want %q", got, want)
	}

	t.Setenv("CAMP_MACHINES_PATH", "/explicit/override/machines.yaml")
	if got, want := MachinesPath(), "/explicit/override/machines.yaml"; got != want {
		t.Fatalf("MachinesPath() with override = %q, want %q", got, want)
	}
}
