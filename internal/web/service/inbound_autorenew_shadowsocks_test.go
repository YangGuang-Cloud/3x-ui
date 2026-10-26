package service

import (
	"testing"
)

// TestAPIUserFromClientCarriesShadowsocksCipher pins what the renew path must
// hand the runtime API. A shadowsocks client object holds no cipher of its own,
// and xray's legacy and 2022 inbounds take different account types that they
// cast without checking — an account built for the wrong one panics the core.
func TestAPIUserFromClientCarriesShadowsocksCipher(t *testing.T) {
	client := map[string]any{"email": "a@x", "password": "pw", "method": "aes-256-gcm"}

	user := apiUserFromClient(client, "aes-256-gcm")
	if got := user["cipher"]; got != "aes-256-gcm" {
		t.Fatalf("cipher = %v, want the inbound method", got)
	}
	if _, polluted := client["cipher"]; polluted {
		t.Fatal("the stored client object was mutated; the API-only cipher would be persisted into the inbound settings")
	}

	user["email"] = "changed@x"
	if client["email"] != "a@x" {
		t.Fatal("the API user shares storage with the stored client object")
	}
}

func TestAPIUserFromClientWithoutCipher(t *testing.T) {
	client := map[string]any{"email": "a@x", "id": "11111111-1111-1111-1111-111111111111"}
	user := apiUserFromClient(client, "")
	if _, ok := user["cipher"]; ok {
		t.Fatal("a non-shadowsocks client must not gain a cipher key")
	}
	if user["id"] != client["id"] {
		t.Fatalf("id = %v, want it copied from the client", user["id"])
	}
}

// TestAutoRenewShadowsocksKeepsSettingsClean renews a shadowsocks client and
// checks the inbound settings the panel writes back: the cipher the API needs
// must not leak into a stored client object, where it would end up in the
// generated xray config as a per-user key.
