package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
)

// AutoCLIOptions generates the module's CLI directly from the gRPC service
// descriptors, so every RPC has a CLI command by construction — the CLI can
// never drift behind the query or tx surface (see CLAUDE.md "CLI/Query and
// CLI/Tx Parity"). Aliases preserve the pre-autocli command names that
// scripts and docs already use.
//
// Tx commands are generated too, with two kinds of exception that stay
// hand-written in client/cli/tx.go: commands with genuine client-side logic
// (user-request-guardians: detection-hint tooling and timing defaults;
// user-distribute-shares: shares-file parsing), and commands whose messages carry
// a Coin field (guardian-register, guardian-update), which client/v2's coin
// flag cannot bind for modules without pulsar-generated types. Each is
// declared here with Skip: true so this descriptor remains the single
// inventory of the tx surface; TestTxCommandParity (cmd/timeflared)
// enforces that every Msg RPC is either generated or
// skipped-with-a-hand-written-command.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "timeflare.secrets.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:      "Secret",
					Use:            "secret [secret-id]",
					Alias:          []string{"show"},
					Short:          "Query a secret's full assembled view (record joined with assignments and reveals)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod: "Secrets",
					Use:       "secrets",
					Alias:     []string{"list-secrets"},
					Short:     "Query all secrets (assembled views, paginated)",
				},
				{
					RpcMethod:      "SecretsByCreator",
					Use:            "secrets-by-creator [creator]",
					Short:          "Query a creator's secrets (assembled views, paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "creator"}},
				},
				{
					RpcMethod: "PendingSecrets",
					Use:       "pending-secrets",
					Short:     "Query secrets in their active reveal phase (pending/reconstructable)",
				},
				{
					RpcMethod:      "SecretMeta",
					Use:            "secret-meta [secret-id]",
					Short:          "Query only a secret's slim metadata record (no share or reveal data)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod:      "SecretAssignments",
					Use:            "secret-assignments [secret-id]",
					Short:          "Query a secret's per-guardian assignment status records",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod:      "SecretReveals",
					Use:            "secret-reveals [secret-id]",
					Short:          "Query a secret's revealed shares",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod: "SecretShare",
					Use:       "secret-share [secret-id] [guardian-address]",
					Short:     "Query a single guardian's encrypted share for a secret",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
						{ProtoField: "guardian_address"},
					},
				},
				{
					RpcMethod:      "SecretPayload",
					Use:            "secret-payload [secret-id]",
					Short:          "Query a secret's stored payload ciphertext (for reconstruction)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod:      "SecretTombstone",
					Use:            "secret-tombstone [secret-id]",
					Short:          "Query the permanent tombstone of a pruned secret",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "secret_id"}},
				},
				{
					RpcMethod:      "HintsSince",
					Use:            "hints-since [since-height]",
					Short:          "Query the discovery-scan feed: (secret-id, detection-hint) records for secrets created at or after the height, in creation order (paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "since_height"}},
				},
				{
					RpcMethod:      "Guardian",
					Use:            "guardian [address]",
					Alias:          []string{"show-guardian"},
					Short:          "Query a guardian by address",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "Guardians",
					Use:       "guardians",
					Alias:     []string{"list-guardians"},
					Short:     "Query all guardians (paginated)",
				},
				{
					RpcMethod:      "GuardianKeyHistory",
					Use:            "guardian-key-history [address]",
					Short:          "Query a guardian's key-epoch history (epoch 0 = the registration key)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "timeflare.secrets.v1.Msg",
			// Merge the generated commands into the module's custom GetTxCmd
			// tree (client/cli/tx.go) instead of skipping the module because
			// a custom tx command exists.
			EnhanceCustomCommand: true,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "GuardianWithdrawStake",
					Use:       "guardian-withdraw-stake",
					Short:     "Withdraw the --from guardian's entire unlocked float (bonds stay locked)",
				},
				{
					RpcMethod: "GuardianConfirmShares",
					Use:       "guardian-confirm-shares [secret-id] [accept]",
					Short:     "Accept or reject a guardian assignment (Phase 3; acceptance locks the bond)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
						{ProtoField: "accept"},
					},
				},
				{
					RpcMethod: "GuardianRevealShare",
					Use:       "guardian-reveal-share [secret-id] [decrypted-share]",
					Short:     "Reveal a guardian's key share during the reveal window (hex, base64, or file path)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
						{ProtoField: "decrypted_share"},
					},
				},
				{
					RpcMethod: "SlashGuardian",
					Use:       "slash-guardian [guardian-address] [evidence] [reason] [secret-id]",
					Short:     "Report a guardian's early reveal with cryptographic evidence (hex, base64, or file path)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "guardian_address"},
						{ProtoField: "evidence"},
						{ProtoField: "reason"},
						{ProtoField: "secret_id"},
					},
				},
				{
					RpcMethod: "GuardianRotateKey",
					Use:       "guardian-rotate-key [new-key]",
					Short:     "Rotate the --from guardian's share-encryption key forward for future assignments (hex, base64, or file path)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "new_key"},
					},
				},
				{
					RpcMethod: "UserCancelSecret",
					Use:       "user-cancel-secret [secret-id]",
					Short:     "Cancel an activated (pending) secret before its reveal window opens",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
					},
				},
				{
					RpcMethod: "RecipientCommitRebate",
					Use:       "recipient-commit-rebate [secret-id] [commitment]",
					Short:     "Commit to collecting a revealed secret's rebate (step 1 of 2)",
					Long: "Publish the commitment binding your recipiency proof to this address, " +
						"which must land at least one block before the reveal. The commitment is " +
						"SHA256(\"timeflare/rebate-commit/v1\" || z || your address bytes), where " +
						"z = X25519(your recipient private key, the secret's hint ephemeral key). " +
						"Committing first is what stops an observer copying your proof out of the " +
						"mempool and collecting the rebate ahead of you.",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
						{ProtoField: "commitment"},
					},
				},
				{
					RpcMethod: "RecipientCollectRebate",
					Use:       "recipient-collect-rebate [secret-id] [proof]",
					Short:     "Reveal the recipiency proof and collect the rebate (step 2 of 2)",
					Long: "Reveal z = X25519(your recipient private key, the secret's hint ephemeral " +
						"key) and collect the rebate. Requires a commitment from an earlier block " +
						"(recipient-commit-rebate). WARNING: the proof is public and permanent once " +
						"submitted — it links this address to that secret, and collecting on several " +
						"secrets links them to one another.",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "secret_id"},
						{ProtoField: "z"},
					},
				},
				// Hand-written commands (client/cli/tx.go) — declared here so
				// the descriptor stays the single inventory of the tx surface.
				{
					RpcMethod: "UserRequestGuardians",
					Skip:      true, // hand-written: detection-hint tooling (--random-hint) + timing defaults
				},
				{
					RpcMethod: "UserDistributeShares",
					Skip:      true, // hand-written: shares-file JSON parsing
				},
				// Coin-blocked: client/v2's coin flag returns a pulsar Coin
				// that dynamicpb rejects for modules without pulsar-generated
				// types ("field descriptor does not belong to this message"
				// panic; verified through client/v2 v2.0.0-beta.11). Generate
				// these once the coin flag binds against the field's own
				// descriptor.
				{
					RpcMethod: "GuardianRegister",
					Skip:      true, // hand-written: Coin deposit field
				},
				{
					RpcMethod: "GuardianUpdate",
					Skip:      true, // hand-written: Coin deposit field
				},
			},
		},
	}
}
