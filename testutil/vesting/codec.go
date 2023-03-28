package vesting

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
)

// RegisterLegacyAminoCodec registers the vesting interfaces and concrete types on the
// provided LegacyAmino codec. These types are used for Amino JSON serialization
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MockVestedDelegateVestingAccount{}, "cosmos-sdk/BaseVestingAccount", nil)
}

// RegisterInterface associates protoName with AccountI and VestingAccount
// Interfaces and creates a registry of it's concrete implementations
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterInterface(
		"cosmos.vesting.v1beta1.VestingAccount",
		(*exported.VestingAccount)(nil),
		&MockVestedDelegateVestingAccount{},
	)

	registry.RegisterImplementations(
		(*authtypes.AccountI)(nil),
		&MockVestedDelegateVestingAccount{},
	)

	registry.RegisterImplementations(
		(*authtypes.GenesisAccount)(nil),
		&MockVestedDelegateVestingAccount{},
	)
}
