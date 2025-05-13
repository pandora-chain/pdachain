package anchor_network

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type AnchorNetworkInfo struct {
	ChainID         *big.Int       `json:"chainID"`
	Name            string         `json:"name"`
	ForkBlockNumber *big.Int       `json:"forkBlockNumber"`
	GenesisAddress  common.Address `json:"genesisAddress"`
	AnchorContract  common.Address `json:"anchorContract"`
	ManagerAddress  common.Address `json:"managerAddress"`
}
