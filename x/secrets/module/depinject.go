package module

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

func init() {
	appmodule.Register(
		&types.Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Config       *types.Module
	Cdc          codec.Codec
	StoreService store.KVStoreService
	AddressCodec address.Codec

	AccountKeeper keeper.AuthKeeper
	BankKeeper    keeper.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	SecretsKeeper keeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(
		in.Cdc,
		in.AddressCodec,
		in.StoreService,
		in.AccountKeeper,
		in.BankKeeper,
	)
	m := NewAppModule(
		in.Cdc,
		k,
		in.AccountKeeper,
		in.BankKeeper,
	)

	return ModuleOutputs{SecretsKeeper: k, Module: m}
}
