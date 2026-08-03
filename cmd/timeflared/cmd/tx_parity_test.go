package cmd_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	secretsmodule "github.com/timeflareio/chain/x/secrets/module"
	"github.com/timeflareio/chain/x/secrets/types"

	"github.com/timeflareio/chain/cmd/timeflared/cmd"
)

// kebabCase converts an RPC method name (UserRequestGuardians) to autocli's
// default command name (user-request-guardians).
func kebabCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestTxCommandParity is the tx half of the CLI parity guarantee (CLAUDE.md
// "CLI/Query and CLI/Tx Parity"): every RPC on the Msg service must be
// deliberately placed — either generated from the service descriptor, or
// declared Skip in AutoCLIOptions and backed by a hand-written command of
// the same (kebab-case) name in client/cli. A new Msg RPC fails this test
// until it is placed on one side; deleting a hand-written command without
// retiring its Skip entry fails it too. The reverse direction catches
// descriptor entries that no longer match any RPC (e.g. after a rename).
func TestTxCommandParity(t *testing.T) {
	rootCmd := cmd.NewRootCmd()
	txCmd, _, err := rootCmd.Find([]string{"tx", types.ModuleName})
	if err != nil {
		t.Fatalf("finding 'tx %s': %v", types.ModuleName, err)
	}
	subcommands := map[string]bool{}
	for _, c := range txCmd.Commands() {
		subcommands[c.Name()] = true
	}

	txOpts := secretsmodule.AppModule{}.AutoCLIOptions().Tx
	if txOpts == nil {
		t.Fatal("AutoCLIOptions declares no Tx service descriptor")
	}
	entries := map[string]bool{}
	for _, o := range txOpts.RpcCommandOptions {
		entries[o.RpcMethod] = true
		var want string
		if o.Skip {
			// Skip promises a hand-written command under autocli's default
			// name for the method.
			want = kebabCase(o.RpcMethod)
		} else {
			want = strings.Fields(o.Use)[0]
		}
		if !subcommands[want] {
			t.Errorf("RPC %s: expected subcommand %q under 'tx %s' (skip=%v), not found",
				o.RpcMethod, want, types.ModuleName, o.Skip)
		}
	}

	desc, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(txOpts.Service))
	if err != nil {
		t.Fatalf("resolving service %s: %v", txOpts.Service, err)
	}
	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("%s is not a service descriptor", txOpts.Service)
	}
	methods := map[string]bool{}
	for i := 0; i < sd.Methods().Len(); i++ {
		name := string(sd.Methods().Get(i).Name())
		methods[name] = true
		if !entries[name] {
			t.Errorf("Msg RPC %s has no RpcCommandOptions entry in AutoCLIOptions — "+
				"add a generated command, or Skip it and hand-write the command in client/cli", name)
		}
	}
	for m := range entries {
		if !methods[m] {
			t.Errorf("RpcCommandOptions entry %q matches no RPC on %s (renamed or removed?)", m, txOpts.Service)
		}
	}
}

// TestUserDistributeSharesConstructsValidMessage pins the hand-written
// user-distribute-shares command against MsgUserDistributeShares.ValidateBasic by
// running it end-to-end in generate-only mode. This is the regression guard
// for the drift that motivated the tx-autocli plan: the command silently
// stopped constructing a valid message when the key-share architecture added
// required fields (payload_ciphertext, secret_public_key).
func TestUserDistributeSharesConstructsValidMessage(t *testing.T) {
	rootCmd := cmd.NewRootCmd()

	home := t.TempDir()
	from := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20)).String()

	randHex := func(n int) string {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}

	sharesFile := filepath.Join(home, "shares.json")
	sharesJSON, err := json.Marshal([]map[string]string{{
		"guardian_address": from,
		"encrypted_share":  randHex(94),
		"share_hmac":       randHex(32),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharesFile, sharesJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	payloadFile := filepath.Join(home, "payload.bin")
	payload := make([]byte, 200)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payloadFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	// The generated tx is printed via the client context, which writes to
	// process stdout regardless of cobra's out writer — capture it.
	realStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })

	out := new(strings.Builder)
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)
	rootCmd.SetArgs([]string{
		"tx", types.ModuleName, "user-distribute-shares",
		"3b8f34f0-8a6a-4c5e-9f3d-2b1a0c9d8e7f", // well-formed UUID
		sharesFile,
		randHex(32), // secret_commitment
		payloadFile, // payload_ciphertext via file path
		randHex(32), // secret_public_key
		"--from", from,
		"--generate-only", "--offline",
		"--account-number", "0", "--sequence", "0",
		"--keyring-backend", "test", "--home", home,
	})

	execErr := svrcmd.Execute(rootCmd, clienthelpers.EnvPrefix, home)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = realStdout
	stdout, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if execErr != nil {
		t.Fatalf("user-distribute-shares generate-only failed (ValidateBasic rejected the constructed message?): %v\noutput: %s%s", execErr, out.String(), stdout)
	}
	if want := fmt.Sprintf("/%s.MsgUserDistributeShares", "timeflare.secrets.v1"); !strings.Contains(string(stdout), want) {
		t.Fatalf("generated tx does not contain %s: %s%s", want, out.String(), stdout)
	}
}
