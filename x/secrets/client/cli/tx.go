package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/spf13/cobra"

	"github.com/timeflareio/chain/x/secrets/types"
)

// flagRandomHint requests a random detection hint (the no-discovery pattern)
// instead of a positional SDK-derived hint on user-request-guardians.
const flagRandomHint = "random-hint"

// parseHexArg decodes a hex-encoded string argument, returning a descriptive error on failure.
func parseHexArg(s, fieldName string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	return b, nil
}

// parseFileOrHexArg resolves an argument that may be a file path (raw bytes)
// or a hex string — the same convention client/v2's binary flag type uses for
// generated commands.
func parseFileOrHexArg(s, fieldName string) ([]byte, error) {
	if b, err := os.ReadFile(s); err == nil {
		return b, nil
	}
	return parseHexArg(s, fieldName)
}

// GetTxCmd returns the module's hand-written transaction commands. Only two
// kinds of command live here — those with genuine client-side logic
// (user-request-guardians: detection-hint tooling and timing defaults;
// user-distribute-shares: shares-file parsing) and those whose messages carry a
// Coin field (guardian-register, guardian-update): client/v2's coin flag
// returns a pulsar Coin that dynamicpb rejects for modules without
// pulsar-generated types (verified through client/v2 v2.0.0-beta.11), so
// they cannot be generated yet. Every other tx command is generated from
// the Msg service descriptor via AutoCLIOptions (module/autocli.go, Skip
// entries name this file), and TestTxCommandParity (cmd/timeflared)
// enforces the split in both directions.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdUserRequestGuardians())
	cmd.AddCommand(CmdUserDistributeShares())
	cmd.AddCommand(CmdGuardianRegister())
	cmd.AddCommand(CmdGuardianUpdate())

	return cmd
}

// CmdGuardianRegister returns a CLI command for registering a guardian.
// Hand-written solely because of the Coin deposit field (see GetTxCmd).
func CmdGuardianRegister() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guardian-register [guardian-address] [encryption-public-key] [available-from] [available-until] [deposit] [accepting-secrets]",
		Short: "Register a new guardian",
		Long: `Register a new guardian with the specified parameters.

Registration charges the protocol entry fee (1,000 VEIL, paid to validators) from the guardian's
account in addition to the initial float deposit.

Parameters:
  - guardian-address: The guardian's blockchain address
  - encryption-public-key: Hex-encoded 32-byte public key for encryption
  - available-from: Blocks from current when guardian becomes available (0 = current block + 1)
  - available-until: Blocks from current when guardian stops being available
  - deposit: Initial float deposit (e.g., "100000000000uveil"; bonds are locked from this per accepted secret)
  - accepting-secrets: (Optional) Whether guardian accepts new secret assignments (default: true)

Examples:
  # Register new guardian with a 100,000 VEIL float, accepting secrets
  timeflared tx secrets guardian-register tmflr1... abc123... 0 1000 100000000000uveil true

  # Register new guardian NOT accepting secrets initially
  timeflared tx secrets guardian-register tmflr1... abc123... 0 1000 50000000000uveil false`,
		Args: cobra.RangeArgs(5, 6), // Accept 5 or 6 arguments
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			guardianAddress := args[0]

			// Parse encryption public key (hex string to bytes)
			encryptionPublicKey, err := parseHexArg(args[1], "encryption public key")
			if err != nil {
				return err
			}

			// Parse available from
			availableFrom, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid available_from: %w", err)
			}

			// Parse available until
			availableUntil, err := strconv.ParseInt(args[3], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid available_until: %w", err)
			}

			// Parse initial float deposit
			deposit, err := sdk.ParseCoinNormalized(args[4])
			if err != nil {
				return fmt.Errorf("invalid deposit: %w", err)
			}

			// Parse accepting_secrets (optional, defaults to true)
			acceptingSecrets := true
			if len(args) >= 6 {
				acceptingSecrets, err = strconv.ParseBool(args[5])
				if err != nil {
					return fmt.Errorf("invalid accepting_secrets: %w", err)
				}
			}

			msg := types.NewMsgGuardianRegister(
				guardianAddress,
				encryptionPublicKey,
				availableFrom,
				availableUntil,
				deposit,
				acceptingSecrets,
			)

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// CmdGuardianUpdate returns a CLI command for updating a guardian.
// Hand-written solely because of the Coin deposit field (see GetTxCmd).
func CmdGuardianUpdate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guardian-update [guardian-address] [flags]",
		Short: "Update an existing guardian's parameters",
		Long: `Update an existing guardian's parameters. All parameters are optional.

Examples:
  # Update availability window
  timeflared tx secrets guardian-update tmflr1... --available-from 0 --available-until 2000

  # Deposit an additional 5,000 VEIL into the float
  timeflared tx secrets guardian-update tmflr1... --deposit 5000000000uveil

  # Note: Encryption keys are permanently immutable and cannot be updated

  # Stop accepting new secrets
  timeflared tx secrets guardian-update tmflr1... --accepting-secrets=false`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			guardianAddress := args[0]

			msg := &types.MsgGuardianUpdate{
				Guardian: guardianAddress,
			}

			// Note: Encryption key updates removed - keys are permanently immutable

			// Parse optional availability window
			availableFrom, err := cmd.Flags().GetInt64("available-from")
			if err != nil {
				return err
			}
			msg.AvailableFrom = availableFrom

			availableUntil, err := cmd.Flags().GetInt64("available-until")
			if err != nil {
				return err
			}
			msg.AvailableUntil = availableUntil

			// Parse optional float deposit top-up
			depositStr, err := cmd.Flags().GetString("deposit")
			if err != nil {
				return err
			}
			if depositStr != "" {
				deposit, err := sdk.ParseCoinNormalized(depositStr)
				if err != nil {
					return fmt.Errorf("invalid deposit: %w", err)
				}
				msg.Deposit = &deposit
			}

			// Parse optional accepting secrets (presence-aware: only set when
			// the flag is explicitly provided, so omission means "no change")
			if cmd.Flags().Changed("accepting-secrets") {
				acceptingSecrets, err := cmd.Flags().GetBool("accepting-secrets")
				if err != nil {
					return err
				}
				msg.AcceptingSecrets = &gogotypes.BoolValue{Value: acceptingSecrets}
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	// Add flags for optional parameters
	// Note: encryption-key flag removed - encryption keys are permanently immutable
	cmd.Flags().Int64("available-from", 0, "Blocks from current when guardian becomes available (0 = preserve existing)")
	cmd.Flags().Int64("available-until", 0, "Blocks from current when guardian stops being available")
	cmd.Flags().String("deposit", "", "Additional float deposit (e.g., 5000000000uveil)")
	cmd.Flags().Bool("accepting-secrets", true, "Whether to accept new secrets")

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// CmdUserRequestGuardians returns a CLI command for requesting guardians (Phase 1)
func CmdUserRequestGuardians() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-request-guardians [detection-hint] [threshold] [min-shares] [max-shares] [bump] [reveal-start-offset] [reveal-duration]",
		Short: "Request guardian assignments for a new secret (Phase 1)",
		Long: `Request guardian assignments for a new secret (Phase 1 of the three-phase commit protocol).

The reward pool is derived by protocol formula (P = rate × distance × max_shares × bump)
and escrowed from your account — you choose the security factor, not the amount.
The pool is fixed: band slots left unfilled at activation are never refunded.
The secret ID is protocol-assigned (read it from the transaction response or the
secret_reserved event) and guardian selection is fully protocol-controlled — the
creator supplies no selection input of any kind.

The guardian band [min-shares, max-shares] is validated as
threshold <= min <= max <= 32 with max - min < threshold (strict): max
candidates are selected and receive shares; at least min must accept by the
commit deadline for the secret to activate with exactly the accepted set.

Parameters:
  - detection-hint: 40 bytes hex (80 chars) — ephemeral public key R (32B) followed by
    the 8B tag, derived from the recipient's public key by the SDK tooling. The
    recipient's key itself is never sent to the chain. Omit this argument and pass
    --random-hint for a secret with no hint-based discovery (random bytes are
    indistinguishable from a real hint by design)
  - bump: Security factor in hundredths (100-1000 = 1.00-10.00). Scales the reward pool and each guardian's bond
  - reveal-start-offset: Blocks from now until reveals start (minimum 100 — the
    fixed commit window plus the reveal buffer)
  - reveal-duration: Duration of the reveal window in blocks (minimum 100; a
    runnable invocation supplies both timing arguments)

Examples:
  # Request guardians with an SDK-derived detection hint:
  # threshold 5, band [6, 9], bump 1.00, reveals open 150 blocks out for 150 blocks
  timeflared tx secrets user-request-guardians <80-hex-chars> 5 6 9 100 150 150 --from creator

  # Devnet/testing: no discovery (--random-hint replaces the positional hint), bump 2.50
  timeflared tx secrets user-request-guardians --random-hint 5 6 9 250 150 150 --from creator`,
		Args: cobra.RangeArgs(4, 7),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			randomHint, err := cmd.Flags().GetBool(flagRandomHint)
			if err != nil {
				return err
			}

			// The detection hint is R(32B) || tag(8B). With --random-hint the
			// positional argument is omitted and random bytes are used (the
			// no-discovery pattern — indistinguishable from a real hint).
			var hint []byte
			if randomHint {
				hint = make([]byte, 40)
				if _, err := rand.Read(hint); err != nil {
					return fmt.Errorf("failed to generate random hint: %w", err)
				}
			} else {
				hint, err = parseHexArg(args[0], "detection hint")
				if err != nil {
					return err
				}
				if len(hint) != 40 {
					return fmt.Errorf("detection hint must be exactly 40 bytes (32B ephemeral key + 8B tag), got %d", len(hint))
				}
				args = args[1:]
			}
			if len(args) < 4 {
				return fmt.Errorf("threshold, min-shares, max-shares and bump are required")
			}

			threshold, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid threshold: %w", err)
			}

			minShares, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid min-shares: %w", err)
			}

			maxShares, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid max-shares: %w", err)
			}

			bump, err := strconv.ParseInt(args[3], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid bump: %w", err)
			}

			// Handle optional parameters
			var revealStartOffset int64 = types.MinRevealStartOffsetTotal // default: minimum valid offset
			var revealDuration int64                                      // must be provided explicitly

			// Parse optional reveal-start-offset (5th remaining argument)
			if len(args) >= 5 {
				revealStartOffset, err = strconv.ParseInt(args[4], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid reveal-start-offset: %w", err)
				}
			}

			// Parse optional reveal-duration (6th remaining argument)
			if len(args) >= 6 {
				revealDuration, err = strconv.ParseInt(args[5], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid reveal-duration: %w", err)
				}
			}

			// Validate that reveal duration is provided and meets minimum requirements
			if revealDuration < types.MinRevealDuration {
				return fmt.Errorf("reveal duration must be at least %d blocks (got %d)", types.MinRevealDuration, revealDuration)
			}

			ephemeralPub, tag := hint[:32], hint[32:40]

			msg := &types.MsgUserRequestGuardians{
				Creator: clientCtx.GetFromAddress().String(),
				DetectionHint: types.DetectionHint{
					Version:      types.DetectionHintVersion,
					EphemeralPub: ephemeralPub,
					Tag:          tag,
				},
				Threshold: threshold,
				MinShares: minShares,
				MaxShares: maxShares,
				Bump:      bump,
				RevealWindow: &types.RevealWindow{
					StartOffset: revealStartOffset,
					Duration:    revealDuration,
				},
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Bool(flagRandomHint, false, "use a random detection hint (no hint-based discovery) instead of a positional hint argument")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// ShareFileEntry represents a single share entry in the JSON file
type ShareFileEntry struct {
	GuardianAddress string `json:"guardian_address"`
	EncryptedShare  string `json:"encrypted_share"`
	ShareHmac       string `json:"share_hmac"`
}

// CmdUserDistributeShares returns a CLI command for distributing shares (Phase 2)
func CmdUserDistributeShares() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-distribute-shares [secret-id] [shares-file] [secret-commitment] [payload-ciphertext] [secret-public-key]",
		Short: "Distribute encrypted key shares and the payload ciphertext to guardians (Phase 2)",
		Long: `Distribute encrypted key shares to assigned guardians (Phase 2 of the three-phase commit protocol).

The shares-file should be a JSON file containing an array of share objects with the following structure:
[
  {
    "guardian_address": "tmflr1...",
    "encrypted_share": "abcdef...",
    "share_hmac": "123456..."
  },
  ...
]

Arguments:
  - secret-commitment: SHA256 of the recipient-encrypted payload C_r (32 bytes hex)
  - payload-ciphertext: C — the doubly-encrypted payload, stored on chain exactly
    once. A file path (raw bytes) or a hex string
  - secret-public-key: pk_s — the per-secret public key (32 bytes hex)

Example:
  timeflared tx secrets user-distribute-shares secret-123 shares.json abcdef1234... payload.bin 0123ab... --from creator`,
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			secretId := args[0]

			// Read and parse shares from JSON file
			sharesFile := args[1]
			fileBytes, err := os.ReadFile(sharesFile)
			if err != nil {
				return fmt.Errorf("failed to read shares file %s: %w", sharesFile, err)
			}

			var shareEntries []ShareFileEntry
			if err := json.Unmarshal(fileBytes, &shareEntries); err != nil {
				return fmt.Errorf("failed to parse shares JSON file: %w", err)
			}

			if len(shareEntries) == 0 {
				return fmt.Errorf("no shares found in file")
			}

			// Convert to protobuf format
			var shares []*types.EncryptedShareData
			for i, entry := range shareEntries {
				encryptedShare, err := parseHexArg(entry.EncryptedShare, fmt.Sprintf("encrypted_share in entry %d", i))
				if err != nil {
					return err
				}

				shareHmac, err := parseHexArg(entry.ShareHmac, fmt.Sprintf("share_hmac in entry %d", i))
				if err != nil {
					return err
				}

				shares = append(shares, &types.EncryptedShareData{
					GuardianAddress: entry.GuardianAddress,
					EncryptedShare:  encryptedShare,
					ShareHmac:       shareHmac,
				})
			}

			secretCommitment, err := parseHexArg(args[2], "secret commitment")
			if err != nil {
				return err
			}

			payloadCiphertext, err := parseFileOrHexArg(args[3], "payload ciphertext")
			if err != nil {
				return err
			}

			secretPublicKey, err := parseHexArg(args[4], "secret public key")
			if err != nil {
				return err
			}

			msg := &types.MsgUserDistributeShares{
				Creator:           clientCtx.GetFromAddress().String(),
				SecretId:          secretId,
				SecretCommitment:  secretCommitment,
				Shares:            shares,
				PayloadCiphertext: payloadCiphertext,
				SecretPublicKey:   secretPublicKey,
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
