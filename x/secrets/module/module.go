package module

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"github.com/timeflareio/chain/x/secrets/client/cli"
	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

var (
	_ module.AppModuleBasic     = (*AppModule)(nil)
	_ module.AppModuleGenesis   = (*AppModule)(nil)
	_ module.AppModule          = (*AppModule)(nil) //nolint:staticcheck // the appmodule extension-interface migration is its own change, not part of the sdk bump
	_ module.HasABCIEndBlock    = (*AppModule)(nil)
	_ appmodule.HasBeginBlocker = (*AppModule)(nil)
)

// AppModuleBasic defines the basic application module used by the secrets module.
type AppModuleBasic struct {
	cdc codec.Codec
}

func NewAppModuleBasic(cdc codec.Codec) AppModuleBasic {
	return AppModuleBasic{cdc: cdc}
}

// Name returns the secrets module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the secrets module's types on the LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the module's interface types
func (a AppModuleBasic) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

// DefaultGenesis returns default genesis state as raw bytes for the secrets module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the secrets module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns the module's hand-written transaction commands — those
// with genuine client-side logic (user-request-guardians, user-distribute-shares) and
// those blocked from generation by client/v2's coin flag (guardian-register,
// guardian-update; see client/cli/tx.go). All other tx commands are
// generated from the Msg service descriptor via AutoCLIOptions (autocli.go,
// EnhanceCustomCommand merges them into this tree), exactly as every query
// command is generated from the query service descriptor. The parity is
// enforced by TestTxCommandParity (cmd/timeflared).
func (a AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// AppModule implements the AppModule interface for the secrets module.
type AppModule struct {
	AppModuleBasic

	keeper        keeper.Keeper
	accountKeeper keeper.AccountKeeper
	bankKeeper    keeper.BankKeeper
}

func NewAppModule(
	cdc codec.Codec,
	keeper keeper.Keeper,
	accountKeeper keeper.AccountKeeper,
	bankKeeper keeper.BankKeeper,
) AppModule {
	return AppModule{
		AppModuleBasic: NewAppModuleBasic(cdc),
		keeper:         keeper,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
	}
}

// RegisterServices registers a GRPC query service to respond to the module-specific GRPC queries.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))

	// 1 → 2: build the guardian selection eligibility index.
	//
	// The index is DERIVED state. InitGenesis populates it as a side effect of
	// writing guardians, but an in-place upgrade never runs InitGenesis — so
	// without this migration a chain whose store predates the index would resume
	// with an empty one and fail every MsgUserRequestGuardians with
	// "insufficient guardians: need N, have 0": total, and invisible in a diff.
	//
	// A versioned module migration rather than surgery in the upgrade handler,
	// because this must also replay correctly for a node syncing from genesis.
	// RunMigrations drives it from the on-chain version map, so it executes once,
	// in consensus, at the upgrade height.
	if err := cfg.RegisterMigration(types.ModuleName, 1, func(ctx sdk.Context) error {
		return am.keeper.RebuildEligibilityIndex(ctx)
	}); err != nil {
		panic(fmt.Errorf("failed to register secrets 1→2 migration: %w", err))
	}
}

// RegisterInvariants registers the secrets module's invariants.
// NOTE: This method is deprecated but required by the SDK AppModule interface.
// It will be removed once the SDK removes it from the interface.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {} //nolint:staticcheck // Required by SDK AppModule interface

// InitGenesis performs the secrets module's genesis initialization
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	var genState types.GenesisState
	cdc.MustUnmarshalJSON(gs, &genState)

	if err := am.keeper.InitGenesis(ctx, genState); err != nil {
		panic(err)
	}

	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the secrets module's exported genesis state as raw JSON bytes.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genState, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return cdc.MustMarshalJSON(&genState)
}

// ConsensusVersion implements ConsensusVersion.
//
// 2 since the guardian selection eligibility index (July 2026): the store gained
// derived state that an in-place upgrade has to build, so RunMigrations needs a
// version gap to drive the 1 → 2 migration registered in RegisterServices.
// Bumping this without registering a migration for the gap would make
// RunMigrations fail the upgrade outright, which is the correct failure.
func (AppModule) ConsensusVersion() uint64 { return 2 }

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// BeginBlock splits the previous block's transaction fees before
// x/distribution allocates them (this module is ordered before distribution
// in app_config.go BeginBlockers): FeeValidatorPercent forwarded to the
// distribution module, FeeBurnPercent permanently burned — the protocol's
// guaranteed, usage-proportional deflation.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.ProcessFeeSplit(ctx)
}

// EndBlock implements the EndBlock ABCI method to check for expired secrets across all phases
// Consolidated commit timeouts: Both Phase 1 and 2 (RESERVED/AWAITING_ACCEPTANCE → FAILED)
// Reveal window timeouts: Phase 3 (PENDING → slashing)
func (am AppModule) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	// Process expired commits: Both Phase 1 and Phase 2 timeout handling consolidated
	if err := am.keeper.ProcessExpiredCommits(ctx); err != nil {
		return []abci.ValidatorUpdate{}, fmt.Errorf("failed to process expired commits: %w", err)
	}

	// Process reveal window timeouts: Reveal window deadline expired (includes slashing)
	if err := am.keeper.ProcessExpiredRevealWindows(ctx); err != nil {
		return []abci.ValidatorUpdate{}, fmt.Errorf("failed to process expired reveal windows: %w", err)
	}

	// Rebate collection deadlines: void uncollected rebates and return their
	// reservations to the pool while it can still redistribute them
	if err := am.keeper.ProcessExpiredRebates(ctx); err != nil {
		return []abci.ValidatorUpdate{}, fmt.Errorf("failed to process expired rebates: %w", err)
	}

	// Stage 2 retention: prune terminal secrets whose window has lapsed,
	// leaving a permanent tombstone (capped per block)
	if err := am.keeper.ProcessDuePrunes(ctx); err != nil {
		return []abci.ValidatorUpdate{}, fmt.Errorf("failed to process due prunes: %w", err)
	}

	return []abci.ValidatorUpdate{}, nil
}
