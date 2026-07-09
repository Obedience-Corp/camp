package machines

import "testing"

func TestDecodeToleratesUnknownFields(t *testing.T) {
	src := []byte(`
version: 1
region: us-east
machines:
  - id: devbox
    host: devbox.tailnet.ts.net
    auth_method: tailscale-ssh
    future_field: ignored
`)
	f, err := decode(src)
	if err != nil {
		t.Fatalf("decode with unknown fields: %v", err)
	}
	if f.Version != 1 || len(f.Machines) != 1 {
		t.Fatalf("unexpected decode: %+v", f)
	}
	if f.Machines[0].ID != "devbox" || f.Machines[0].AuthMethod != AuthTailscaleSSH {
		t.Fatalf("known fields not preserved: %+v", f.Machines[0])
	}
}

func TestEncodeDropsLocal(t *testing.T) {
	in := &File{Machines: []Machine{
		{ID: LocalMachineID, Host: "127.0.0.1", AuthMethod: AuthSSHAgent},
		{ID: "devbox", Host: "devbox.ts.net", AuthMethod: AuthTailscaleSSH},
	}}
	data, err := encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Version != currentVersion {
		t.Fatalf("version not stamped: %d", out.Version)
	}
	if len(out.Machines) != 1 || out.Machines[0].ID != "devbox" {
		t.Fatalf("local not dropped or devbox lost: %+v", out.Machines)
	}
}

func TestEncodeDecodeRoundTripAllFields(t *testing.T) {
	m := Machine{
		ID:           "devbox",
		Label:        "Dev Box",
		Host:         "devbox.tailnet.ts.net",
		AuthMethod:   AuthSSHAgent,
		SSHUser:      "lance",
		IdentityFile: "~/.ssh/id_ed25519",
	}
	data, err := encode(&File{Machines: []Machine{m}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Machines) != 1 {
		t.Fatalf("machine count = %d", len(out.Machines))
	}
	if out.Machines[0] != m {
		t.Fatalf("round-trip lost fields:\n got %+v\nwant %+v", out.Machines[0], m)
	}
}

func TestLookup(t *testing.T) {
	f := &File{Machines: []Machine{{ID: "devbox", Host: "devbox.ts.net"}}}

	if m, isLocal, found := f.Lookup(LocalMachineID); !isLocal || !found || m != nil {
		t.Fatalf("Lookup(local) = (%v, %v, %v), want (nil, true, true)", m, isLocal, found)
	}
	if m, isLocal, found := f.Lookup(""); !isLocal || !found || m != nil {
		t.Fatalf(`Lookup("") = (%v, %v, %v), want (nil, true, true)`, m, isLocal, found)
	}
	if m, isLocal, found := f.Lookup("devbox"); found != true || isLocal || m == nil || m.ID != "devbox" {
		t.Fatalf("Lookup(devbox) = (%v, %v, %v), want the devbox machine", m, isLocal, found)
	}
	if m, isLocal, found := f.Lookup("nope"); found || isLocal || m != nil {
		t.Fatalf("Lookup(nope) = (%v, %v, %v), want (nil, false, false)", m, isLocal, found)
	}
}

func TestLoadAbsentFileDegradesToEmpty(t *testing.T) {
	t.Setenv("CAMP_MACHINES_PATH", "/nonexistent-mm-test-dir/does-not-exist/machines.yaml")
	f, err := Load()
	if err != nil {
		t.Fatalf("Load() of absent file returned error: %v", err)
	}
	if f == nil || len(f.Machines) != 0 {
		t.Fatalf("absent file did not degrade to zero machines: %+v", f)
	}
	if f.Version != currentVersion {
		t.Fatalf("absent-file File missing version stamp: %d", f.Version)
	}
}
