package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUserRequestGuardians{}, "secrets/MsgUserRequestGuardians", nil)
	cdc.RegisterConcrete(&MsgUserDistributeShares{}, "secrets/MsgUserDistributeShares", nil)
	cdc.RegisterConcrete(&MsgGuardianConfirmShares{}, "secrets/MsgGuardianConfirmShares", nil)
	cdc.RegisterConcrete(&MsgUserCancelSecret{}, "secrets/MsgUserCancelSecret", nil)
	cdc.RegisterConcrete(&MsgGuardianRevealShare{}, "secrets/MsgGuardianRevealShare", nil)
	cdc.RegisterConcrete(&MsgGuardianRegister{}, "secrets/MsgGuardianRegister", nil)
	cdc.RegisterConcrete(&MsgGuardianUpdate{}, "secrets/MsgGuardianUpdate", nil)
	cdc.RegisterConcrete(&MsgGuardianRotateKey{}, "secrets/MsgGuardianRotateKey", nil)
	cdc.RegisterConcrete(&MsgSlashGuardian{}, "secrets/MsgSlashGuardian", nil)
	cdc.RegisterConcrete(&MsgGuardianWithdrawStake{}, "secrets/MsgGuardianWithdrawStake", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUserRequestGuardians{},
		&MsgUserDistributeShares{},
		&MsgGuardianConfirmShares{},
		&MsgUserCancelSecret{},
		&MsgGuardianRevealShare{},
		&MsgGuardianRegister{},
		&MsgGuardianUpdate{},
		&MsgGuardianRotateKey{},
		&MsgSlashGuardian{},
		&MsgGuardianWithdrawStake{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the necessary x/secrets interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	RegisterCodec(cdc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)

func init() {
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
