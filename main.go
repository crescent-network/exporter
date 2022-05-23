package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	chain "github.com/crescent-network/crescent/app"
	chaincmd "github.com/crescent-network/crescent/cmd/crescentd/cmd"
	farmingtypes "github.com/crescent-network/crescent/x/farming/types"
	"github.com/crescent-network/crescent/x/liquidity/amm"
	liquiditytypes "github.com/crescent-network/crescent/x/liquidity/types"
)

var (
	genFile = "crescent-exported-350110.json"

	bankGenState      banktypes.GenesisState
	liquidityGenState liquiditytypes.GenesisState
	farmingGenState   farmingtypes.GenesisState
)

const (
	USTDenom       = "ibc/6F4968A73F90CF7DE6394BF937D6DF7C7D162D74D839C13F53B41157D315E05F"
	LUNADenom      = "ibc/4627AD2524E3E0523047E35BB76CC90E37D9D57ACF14F0FCBCEB2480705F3CB8"
	Pool2CoinDenom = "pool2" // bCRE / UST
	Pool4CoinDenom = "pool4" // LUNA / bCRE
	Pool5CoinDenom = "pool5" // ATOM / UST
	Pool6CoinDenom = "pool6" // LUNA / UST
)

func init() {
	chaincmd.GetConfig()

	appState, _, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		log.Fatalf("failed to read genesis file: %v\n", err)
	}

	encodingCfg := chain.MakeEncodingConfig()
	codec := encodingCfg.Marshaler
	codec.MustUnmarshalJSON(appState[banktypes.ModuleName], &bankGenState)
	codec.MustUnmarshalJSON(appState[liquiditytypes.ModuleName], &liquidityGenState)
	codec.MustUnmarshalJSON(appState[farmingtypes.ModuleName], &farmingGenState)
}

type Result struct {
	LUNA              sdk.Coin // the total LUNA coin for an account
	UST               sdk.Coin // the total UST coin for an account
	Holder            bool     // whether or not an account is either LUNA or UST holder
	LiquidityProvider bool     // whether or not an account is liquidity provider for LUNA or UST pools
	Farmer            bool     // whether or not an account is farmer for LUNA or UST pools
}

type PoolMetaData struct {
	BasicPool *amm.BasicPool
	QuoteCoin sdk.Coin
	BaseCoin  sdk.Coin
}

func main() {
	// pairIdByPair is a cache to store all pairs
	pairIdByPair := map[uint64]liquiditytypes.Pair{} // PairId => Pair
	for _, pair := range liquidityGenState.Pairs {
		if _, ok := pairIdByPair[pair.Id]; !ok {
			pairIdByPair[pair.Id] = liquiditytypes.Pair{}
		}
		pairIdByPair[pair.Id] = pair
	}

	// poolCoinDenomByPool is a cache to store all pools
	poolCoinDenomByPool := map[string]liquiditytypes.Pool{} // PoolCoinDenom => Pool
	for _, pool := range liquidityGenState.Pools {
		if _, ok := poolCoinDenomByPool[pool.PoolCoinDenom]; !ok {
			poolCoinDenomByPool[pool.PoolCoinDenom] = liquiditytypes.Pool{}
		}
		poolCoinDenomByPool[pool.PoolCoinDenom] = pool
	}

	// poolReserveAddressByBasicPool is a cache that stores BasicPool for each pool reserve account
	poolReserveAddressByBasicPool := map[string]PoolMetaData{} // ReserveAddress => PoolMetaData
	for _, balance := range bankGenState.GetBalances() {
		for _, pool := range poolCoinDenomByPool {
			if balance.Address == pool.ReserveAddress {
				pair := pairIdByPair[pool.PairId]
				spendable := balance.GetCoins()
				rx := sdk.NewCoin(pair.QuoteCoinDenom, spendable.AmountOf(pair.QuoteCoinDenom))
				ry := sdk.NewCoin(pair.BaseCoinDenom, spendable.AmountOf(pair.BaseCoinDenom))
				ps := bankGenState.Supply.AmountOf(pool.PoolCoinDenom)
				ammPool := amm.NewBasicPool(rx.Amount, ry.Amount, ps)

				poolReserveAddressByBasicPool[pool.ReserveAddress] = PoolMetaData{
					BasicPool: ammPool,
					QuoteCoin: rx,
					BaseCoin:  ry,
				}
				// fmt.Printf("PoolID: %d; QuoteCoin: %s; BaseCoin: %s\n", pool.Id, rx, ry)
			}
		}
	}

	// addrByResult stores result information
	addrByResult := map[string]Result{} // Address => Result

	// Parse holders and liquidity providers
	for _, balance := range bankGenState.GetBalances() {
		for _, coin := range balance.GetCoins() {
			switch coin.Denom {
			case LUNADenom:
				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:   addrByResult[balance.Address].LUNA.Add(coin),
					UST:    addrByResult[balance.Address].UST,
					Holder: true,
				}
			case USTDenom:
				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:   addrByResult[balance.Address].LUNA,
					UST:    addrByResult[balance.Address].UST.Add(coin),
					Holder: true,
				}
			case Pool2CoinDenom: // bCRE / UST
				pool := poolCoinDenomByPool[coin.Denom]
				ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
				x, _ := ammPool.BasicPool.Withdraw(coin.Amount, liquidityGenState.Params.WithdrawFeeRate)

				if ammPool.QuoteCoin.Denom != USTDenom {
					panic("quote coin denom must be UST")
				}

				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:              addrByResult[balance.Address].LUNA,
					UST:               addrByResult[balance.Address].UST.AddAmount(x),
					LiquidityProvider: true,
				}
			case Pool4CoinDenom: // LUNA / bCRE
				pool := poolCoinDenomByPool[coin.Denom]
				ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
				_, y := ammPool.BasicPool.Withdraw(coin.Amount, liquidityGenState.Params.WithdrawFeeRate)

				if ammPool.BaseCoin.Denom != LUNADenom {
					panic("base coin denom must be  LUNA")
				}

				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:              addrByResult[balance.Address].LUNA.AddAmount(y),
					UST:               addrByResult[balance.Address].UST,
					LiquidityProvider: true,
				}
			case Pool5CoinDenom: // ATOM / UST
				pool := poolCoinDenomByPool[coin.Denom]
				ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
				x, _ := ammPool.BasicPool.Withdraw(coin.Amount, liquidityGenState.Params.WithdrawFeeRate)

				if ammPool.QuoteCoin.Denom != USTDenom {
					panic("quote coin denom must be UST")
				}

				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:              addrByResult[balance.Address].LUNA,
					UST:               addrByResult[balance.Address].UST.AddAmount(x),
					LiquidityProvider: true,
				}
			case Pool6CoinDenom: // LUNA / UST
				pool := poolCoinDenomByPool[coin.Denom]
				ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
				x, y := ammPool.BasicPool.Withdraw(coin.Amount, liquidityGenState.Params.WithdrawFeeRate)

				if ammPool.QuoteCoin.Denom != USTDenom {
					panic("quote coin denom must be UST")
				}
				if ammPool.BaseCoin.Denom != LUNADenom {
					panic("base coin denom must be LUNA")
				}

				if _, ok := addrByResult[balance.Address]; !ok {
					addrByResult[balance.Address] = Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				addrByResult[balance.Address] = Result{
					LUNA:              addrByResult[balance.Address].LUNA.AddAmount(y),
					UST:               addrByResult[balance.Address].UST.AddAmount(x),
					LiquidityProvider: true,
				}
			default:
				continue
			}
		}
	}

	// Parse farmers (StakingRecords)
	// It handles cases for LUNA and UST denomination since there may be some farmers
	// who staked LUNA or UST
	for _, record := range farmingGenState.StakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(record.Staking.Amount),
				UST:    addrByResult[record.Farmer].UST,
				Farmer: true,
			}
		case USTDenom:
			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(record.Staking.Amount),
				Farmer: true,
			}
		case Pool2CoinDenom: // bCRE / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, _ := ammPool.BasicPool.Withdraw(record.Staking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		case Pool4CoinDenom: // LUNA / bCRE
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			_, y := ammPool.BasicPool.Withdraw(record.Staking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.BaseCoin.Denom != LUNADenom {
				panic("base coin denom must be LUNA")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(y),
				UST:    addrByResult[record.Farmer].UST,
				Farmer: true,
			}
		case Pool5CoinDenom: // ATOM / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, _ := ammPool.BasicPool.Withdraw(record.Staking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		case Pool6CoinDenom: // LUNA / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, y := ammPool.BasicPool.Withdraw(record.Staking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}
			if ammPool.BaseCoin.Denom != LUNADenom {
				panic("base coin denom must be LUNA")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(y),
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		default:
			continue
		}
	}

	// Parse farmers (QueuedStakingRecords)
	// It handles cases for LUNA and UST denomination since there may be some farmers
	// who staked LUNA or UST
	for _, record := range farmingGenState.QueuedStakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(record.QueuedStaking.Amount),
				UST:    addrByResult[record.Farmer].UST,
				Farmer: true,
			}
		case USTDenom:
			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(record.QueuedStaking.Amount),
				Farmer: true,
			}
		case Pool2CoinDenom: // bCRE / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, _ := ammPool.BasicPool.Withdraw(record.QueuedStaking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		case Pool4CoinDenom: // LUNA / bCRE
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			_, y := ammPool.BasicPool.Withdraw(record.QueuedStaking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.BaseCoin.Denom != LUNADenom {
				panic("base coin denom must be LUNA")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(y),
				UST:    addrByResult[record.Farmer].UST,
				Farmer: true,
			}
		case Pool5CoinDenom: // ATOM / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, _ := ammPool.BasicPool.Withdraw(record.QueuedStaking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA,
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		case Pool6CoinDenom: // LUNA / UST
			pool := poolCoinDenomByPool[record.StakingCoinDenom]
			ammPool := poolReserveAddressByBasicPool[pool.ReserveAddress]
			x, y := ammPool.BasicPool.Withdraw(record.QueuedStaking.Amount, liquidityGenState.Params.WithdrawFeeRate)

			if ammPool.QuoteCoin.Denom != USTDenom {
				panic("quote coin denom must be UST")
			}
			if ammPool.BaseCoin.Denom != LUNADenom {
				panic("base coin denom must be LUNA")
			}

			if _, ok := addrByResult[record.Farmer]; !ok {
				addrByResult[record.Farmer] = Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			addrByResult[record.Farmer] = Result{
				LUNA:   addrByResult[record.Farmer].LUNA.AddAmount(y),
				UST:    addrByResult[record.Farmer].UST.AddAmount(x),
				Farmer: true,
			}
		default:
			continue
		}
	}

	// Dump result file
	// if err := dump(addrByResult); err != nil {
	// 	panic(err)
	// }
}

func dump(addrByResult map[string]Result) error {
	f, err := os.OpenFile("result.csv", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)

	// Write header
	if err := w.Write([]string{
		"address",
		"luna",
		"ust",
		"holder",
		"liquidity_provider",
		"farmer",
	}); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	if err := w.Error(); err != nil {
		return fmt.Errorf("failed to either write or flush: %w", err)
	}

	var (
		holderNum    int
		lpNum        int
		farmerNum    int
		totalLUNAAmt sdk.Int
		totalUSTAmt  sdk.Int
	)

	totalLUNAAmt = sdk.ZeroInt()
	totalUSTAmt = sdk.ZeroInt()
	for addr, result := range addrByResult {
		switch {
		case result.Holder:
			holderNum++
		case result.LiquidityProvider:
			lpNum++
		case result.Farmer:
			farmerNum++
		}

		if result.LUNA.Denom == LUNADenom {
			totalLUNAAmt = totalLUNAAmt.Add(result.LUNA.Amount)
		}
		if result.UST.Denom == USTDenom {
			totalUSTAmt = totalUSTAmt.Add(result.UST.Amount)
		}

		content := []string{
			addr,
			result.LUNA.String(),
			result.UST.String(),
			fmt.Sprint(result.Holder),
			fmt.Sprint(result.LiquidityProvider),
			fmt.Sprint(result.Farmer),
		}

		if err := w.Write(content); err != nil {
			return err
		}
		log.Printf("🏃 Writing content to %s file...\n", f.Name())
	}
	w.Flush()

	log.Print("| ----------Result--------------------------------------------------")
	log.Printf("| 1. # of Holders: %d\n", holderNum)
	log.Printf("| 2. # of Liquidity Providers: %d\n", lpNum)
	log.Printf("| 3. # of Farmers: %d\n", farmerNum)
	log.Printf("| 4. Total #: %d\n", holderNum+lpNum+farmerNum)
	log.Printf("| 5. Total LUNA Amount: %s\n", totalLUNAAmt)
	log.Printf("| 6. Total UST Amount: %s\n", totalUSTAmt)
	log.Print("| -----------------------------------------------------------------")

	return nil
}
