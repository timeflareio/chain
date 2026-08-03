package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/timeflareio/chain/x/secrets/types"

	"github.com/timeflareio/chain/cmd/timeflared/cmd"
)

// TestExampleDrift is the example half of the CLI parity guarantee: every
// `timeflared …` example in docs/operations.md and in the tx commands' own
// help text must resolve against the real command tree — the command path
// exists, every flag exists, and the positional count satisfies the command's
// Args validator. For user-request-guardians the band/timing arguments are
// additionally run through MsgUserRequestGuardians.ValidateBasic, because its
// arity range is wide enough that a retired argument shape can still pass an
// arity check (the drift that motivated this test: help examples predating
// the [min, max] band parsed as max_shares = 100 and failed validation).
//
// Examples are documentation, and documentation rots silently; this test is
// what makes an example that no longer runs fail CI instead.
func TestExampleDrift(t *testing.T) {
	rootCmd := cmd.NewRootCmd()

	// Source 1: every example in the operations reference.
	opsPath := filepath.Join(repoRootDir(t), "docs", "operations.md")
	opsText, err := os.ReadFile(opsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", opsPath, err)
	}
	opsExamples := extractInvocations(string(opsText))
	// A silent extraction failure must not pass vacuously: operations.md
	// carries an example block for every operation plus the query table.
	if len(opsExamples) < 15 {
		t.Fatalf("extracted only %d examples from docs/operations.md — extraction broken?", len(opsExamples))
	}
	for _, inv := range opsExamples {
		checkInvocation(t, rootCmd, "docs/operations.md", inv)
	}

	// Source 2: the tx commands' own Long/Example help text.
	txCmd, _, err := rootCmd.Find([]string{"tx", types.ModuleName})
	if err != nil {
		t.Fatalf("finding 'tx %s': %v", types.ModuleName, err)
	}
	helpExamples := 0
	for _, c := range txCmd.Commands() {
		for _, inv := range extractInvocations(c.Long + "\n" + c.Example) {
			helpExamples++
			checkInvocation(t, rootCmd, "tx "+c.Name()+" help text", inv)
		}
	}
	if helpExamples < 4 {
		t.Fatalf("extracted only %d examples from tx command help text — extraction broken?", helpExamples)
	}
}

// repoRootDir locates the repository root from this test file's location.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// extractInvocations pulls every `timeflared …` command line out of a block
// of text, joining backslash continuations and tokenising with double-quote
// awareness (a quoted multi-word argument is one token).
func extractInvocations(text string) [][]string {
	lines := strings.Split(text, "\n")
	var invocations [][]string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "timeflared ") {
			continue
		}
		for strings.HasSuffix(line, "\\") && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, "\\") + " " + strings.TrimSpace(lines[i])
		}
		if toks := tokenise(line); len(toks) > 1 {
			invocations = append(invocations, toks[1:]) // drop "timeflared"
		}
	}
	return invocations
}

// tokenise splits a shell-style line on whitespace, keeping double-quoted
// spans as single tokens (quotes stripped).
func tokenise(line string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

// checkInvocation resolves one example against the command tree: descend the
// command path, verify every flag exists, and run the leaf's Args validator
// over the positional arguments.
func checkInvocation(t *testing.T, root *cobra.Command, source string, toks []string) {
	t.Helper()
	display := strings.Join(toks, " ")

	// Descend the command path (leading non-flag tokens matching subcommands).
	cur := root
	i := 0
	for i < len(toks) && !strings.HasPrefix(toks[i], "-") {
		next := findSubcommand(cur, toks[i])
		if next == nil {
			break
		}
		cur = next
		i++
	}
	if cur == root || cur.HasSubCommands() {
		t.Errorf("%s: %q does not resolve to a leaf command", source, display)
		return
	}
	// Flags may not have been registered on generated commands until help/
	// execution paths touch them; InitDefaultHelpFlag mirrors what execution
	// does before flag parsing.
	cur.InitDefaultHelpFlag()

	randomHint := false
	var positionals []string
	for ; i < len(toks); i++ {
		tok := toks[i]
		if !strings.HasPrefix(tok, "--") {
			positionals = append(positionals, tok)
			continue
		}
		name := strings.TrimPrefix(tok, "--")
		hasInlineValue := false
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
			hasInlineValue = true
		}
		flag := cur.Flags().Lookup(name)
		if flag == nil {
			flag = cur.InheritedFlags().Lookup(name)
		}
		if flag == nil {
			t.Errorf("%s: %q uses flag --%s, which the command does not define", source, display, name)
			return
		}
		if name == "random-hint" {
			randomHint = true
		}
		if !hasInlineValue && flag.Value.Type() != "bool" {
			i++ // the flag consumes the next token as its value
		}
	}

	if cur.Args != nil {
		if err := cur.Args(cur, positionals); err != nil {
			t.Errorf("%s: %q fails the command's argument validator: %v", source, display, err)
			return
		}
	}

	if cur.Name() == "user-request-guardians" {
		checkRequestGuardiansExample(t, source, display, positionals, randomHint)
	}
}

// checkRequestGuardiansExample runs an example's band/timing arguments
// through MsgUserRequestGuardians.ValidateBasic. The command's arity range
// (4–7 positionals) is wide enough that a pre-band example still passes an
// arity check while constructing a message the chain rejects, so arity alone
// cannot guard this command's examples.
func checkRequestGuardiansExample(t *testing.T, source, display string, positionals []string, randomHint bool) {
	t.Helper()

	args := positionals
	if !randomHint {
		if len(args) == 0 {
			t.Errorf("%s: %q has no detection-hint argument and no --random-hint", source, display)
			return
		}
		args = args[1:] // drop the hint token (placeholder or hex)
	}
	// threshold, min-shares, max-shares, bump, reveal-start-offset,
	// reveal-duration: the CLI rejects a missing duration, so a runnable
	// example carries all six.
	if len(args) != 6 {
		t.Errorf("%s: %q has %d band/timing arguments; a runnable example needs 6 "+
			"(threshold min-shares max-shares bump reveal-start-offset reveal-duration)",
			source, display, len(args))
		return
	}
	nums := make([]int64, 6)
	for j, a := range args {
		n, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			t.Errorf("%s: %q band/timing argument %q is not numeric", source, display, a)
			return
		}
		nums[j] = n
	}

	// A structurally valid hint: the X25519 base point is never low-order.
	ephemeral := make([]byte, types.PublicKeyLength)
	ephemeral[0] = 9
	msg := &types.MsgUserRequestGuardians{
		Creator: sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20)).String(),
		DetectionHint: types.DetectionHint{
			Version:      types.DetectionHintVersion,
			EphemeralPub: ephemeral,
			Tag:          bytes.Repeat([]byte{0x01}, 8),
		},
		Threshold: nums[0],
		MinShares: nums[1],
		MaxShares: nums[2],
		Bump:      nums[3],
		RevealWindow: &types.RevealWindow{
			StartOffset: nums[4],
			Duration:    nums[5],
		},
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("%s: %q constructs a message the chain rejects: %v", source, display, err)
	}
}

// findSubcommand matches a token against a command's children by name or
// alias.
func findSubcommand(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == name || slices.Contains(sub.Aliases, name) {
			return sub
		}
	}
	return nil
}
