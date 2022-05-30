package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	chain "github.com/crescent-network/crescent/app"
	chaincmd "github.com/crescent-network/crescent/cmd/crescentd/cmd"
	farmingtypes "github.com/crescent-network/crescent/x/farming/types"
	"github.com/crescent-network/crescent/x/liquidity/amm"
	liquiditytypes "github.com/crescent-network/crescent/x/liquidity/types"
)

var (
	genFile    = "crescent-exported-536806.json"
	resultFile = "crescent-exported-536806.csv"

	bankGenState      banktypes.GenesisState
	liquidityGenState liquiditytypes.GenesisState
	farmingGenState   farmingtypes.GenesisState
)

const (
	ATOMDenom        = "ibc/C4CFF46FD6DE35CA4CF4CE031E643C8FDC9BA4B99AE598E9B0ED98FE3A2319F9"
	Pool3CoinDenom   = "pool3" // ATOM / bCRE
	Pool5CoinDenom   = "pool5" // ATOM / UST
	Bech32AddrPrefix = "g"     // https://github.com/gnolang/gno/blob/master/pkgs/crypto/consts.go#L5
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
	ATOM              sdk.Coin // the total amount of ATOM for an account
	Holder            bool     // whether or not an account is ATOM holder
	LiquidityProvider bool     // whether or not an account is liquidity provider for pools that correspond with ATOM
	Farmer            bool     // whether or not an account is farmer for pools that correspond with ATOM
}

type PoolMetaData struct {
	BasicPool *amm.BasicPool
	QuoteCoin sdk.Coin
	BaseCoin  sdk.Coin
}

func main() {
	// pairById is a cache to store all pairs
	pairById := map[uint64]liquiditytypes.Pair{} // PairId => Pair
	for _, pair := range liquidityGenState.Pairs {
		pairById[pair.Id] = pair
	}

	// poolByPoolCoinDenom is a cache to store all pools
	poolByPoolCoinDenom := map[string]liquiditytypes.Pool{} // PoolCoinDenom => Pool
	for _, pool := range liquidityGenState.Pools {
		poolByPoolCoinDenom[pool.PoolCoinDenom] = pool
	}

	// basicPoolByReserveAddress is a cache that stores BasicPool for each pool reserve account
	basicPoolByReserveAddress := map[string]PoolMetaData{} // ReserveAddress => PoolMetaData
	for _, balance := range bankGenState.GetBalances() {
		for _, pool := range poolByPoolCoinDenom {
			if balance.Address == pool.ReserveAddress {
				pair := pairById[pool.PairId]
				spendable := balance.GetCoins()
				rx := sdk.NewCoin(pair.QuoteCoinDenom, spendable.AmountOf(pair.QuoteCoinDenom))
				ry := sdk.NewCoin(pair.BaseCoinDenom, spendable.AmountOf(pair.BaseCoinDenom))
				ps := bankGenState.Supply.AmountOf(pool.PoolCoinDenom)
				ammPool := amm.NewBasicPool(rx.Amount, ry.Amount, ps)

				basicPoolByReserveAddress[pool.ReserveAddress] = PoolMetaData{
					BasicPool: ammPool,
					QuoteCoin: rx,
					BaseCoin:  ry,
				}
			}
		}
	}

	// reserveAddressSet saves all reserve accounts that need to be excluded when iterating GetBalances
	reserveAddressSet := map[string]struct{}{} // ReserveAddress => struct
	for _, pool := range liquidityGenState.Pools {
		reserveAddressSet[pool.ReserveAddress] = struct{}{} // pool reserve address
	}
	reserveAddressSet[liquidityGenState.Params.DustCollectorAddress] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool3CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool5CoinDenom).String()] = struct{}{}

	// getATOMAmt is reusable function when parsing ATOM holders, lps, and farmers.
	getATOMAmt := func(poolCoin sdk.Coin) (atomAmt sdk.Int) {
		atomAmt = sdk.ZeroInt()

		pool := poolByPoolCoinDenom[poolCoin.Denom]
		ammPool := basicPoolByReserveAddress[pool.ReserveAddress]
		_, y := ammPool.BasicPool.Withdraw(poolCoin.Amount, sdk.ZeroDec())

		if ammPool.BaseCoin.Denom == ATOMDenom {
			atomAmt = atomAmt.Add(y)
		}
		return
	}

	// resultByAddress stores result information
	resultByAddress := map[string]*Result{} // Address => Result

	// Parse holders and liquidity providers
	for _, balance := range bankGenState.GetBalances() {
		_, ok := reserveAddressSet[balance.Address] // skip addresses in reserve address set
		if ok {
			continue
		}

		for _, coin := range balance.GetCoins() {
			switch coin.Denom {
			case ATOMDenom:
				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						ATOM: sdk.NewInt64Coin(ATOMDenom, 0),
					}
				}
				resultByAddress[balance.Address].ATOM = resultByAddress[balance.Address].ATOM.Add(coin)
				resultByAddress[balance.Address].Holder = true

			case Pool3CoinDenom, Pool5CoinDenom:
				atomAmt := getATOMAmt(coin)

				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						ATOM: sdk.NewInt64Coin(ATOMDenom, 0),
					}
				}
				resultByAddress[balance.Address].ATOM = resultByAddress[balance.Address].ATOM.AddAmount(atomAmt)
				resultByAddress[balance.Address].LiquidityProvider = true

			default:
				continue
			}
		}
	}

	// Parsing farmers from staking records
	for _, record := range farmingGenState.StakingRecords {
		switch record.StakingCoinDenom {
		case ATOMDenom:
			resultByAddress[record.Farmer].ATOM = resultByAddress[record.Farmer].ATOM.AddAmount(record.Staking.Amount) // NOT FOUND

		case Pool3CoinDenom, Pool5CoinDenom:
			atomAmt := getATOMAmt(sdk.NewCoin(record.StakingCoinDenom, record.Staking.Amount))
			if _, ok := resultByAddress[record.Farmer]; !ok {
				resultByAddress[record.Farmer] = &Result{
					ATOM: sdk.NewInt64Coin(ATOMDenom, 0),
				}
			}
			resultByAddress[record.Farmer].ATOM = resultByAddress[record.Farmer].ATOM.AddAmount(atomAmt)
			resultByAddress[record.Farmer].Farmer = true
		}
	}

	// Parsing farmers from queued staking records
	for _, record := range farmingGenState.QueuedStakingRecords {
		switch record.StakingCoinDenom {
		case ATOMDenom:
			resultByAddress[record.Farmer].ATOM = resultByAddress[record.Farmer].ATOM.AddAmount(record.QueuedStaking.Amount) // NOT FOUND

		case Pool3CoinDenom, Pool5CoinDenom:
			atomAmt := getATOMAmt(sdk.NewCoin(record.StakingCoinDenom, record.QueuedStaking.Amount))
			if _, ok := resultByAddress[record.Farmer]; !ok {
				resultByAddress[record.Farmer] = &Result{
					ATOM: sdk.NewInt64Coin(ATOMDenom, 0),
				}
			}
			resultByAddress[record.Farmer].ATOM = resultByAddress[record.Farmer].ATOM.AddAmount(atomAmt)
			resultByAddress[record.Farmer].Farmer = true
		}
	}

	// Dump result file
	if err := dump(resultByAddress); err != nil {
		panic(err)
	}

	// Verify total supply of LUNA and UST are the almost the same as results
	verify(resultByAddress)
}

func verify(resultByAddress map[string]*Result) {
	holderNum := 0
	lpNum := 0
	farmerNum := 0

	totalATOMAmt := sdk.ZeroInt()
	for _, result := range resultByAddress {
		switch {
		case result.Holder:
			holderNum++
		case result.LiquidityProvider:
			lpNum++
		case result.Farmer:
			farmerNum++
		}
		totalATOMAmt = totalATOMAmt.Add(result.ATOM.Amount)
	}

	log.Println("[Supply]")
	log.Println("Total: ", bankGenState.Supply.AmountOf(ATOMDenom))
	log.Println("")
	log.Println("[Result]")
	log.Println("Total: ", totalATOMAmt)
	log.Println("Total #: ", holderNum+lpNum+farmerNum)
	log.Println("Holders #", holderNum)
	log.Println("LPs #", lpNum)
	log.Println("Farmers #", farmerNum)
	log.Println("")
	log.Println("[Difference due to truncation]")
	log.Println("Diff: ", bankGenState.Supply.AmountOf(ATOMDenom).Sub(totalATOMAmt))
}

func dump(resultByAddress map[string]*Result) error {
	f, err := os.OpenFile(resultFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)

	// Write header
	if err := w.Write([]string{
		"address",
		"address_gno",
		"atom",
		"holder",
		"liquidity_provider",
		"farmer",
	}); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	if err := w.Error(); err != nil {
		return fmt.Errorf("failed to either write or flush: %w", err)
	}

	for addr, result := range resultByAddress {
		_, decoded, err := bech32.DecodeAndConvert(addr)
		if err != nil {
			return err
		}
		convertedAddr, err := bech32.ConvertAndEncode(Bech32AddrPrefix, decoded)
		if err != nil {
			return err
		}

		if err := w.Write([]string{
			addr,
			convertedAddr,
			result.ATOM.String(),
			fmt.Sprint(result.Holder),
			fmt.Sprint(result.LiquidityProvider),
			fmt.Sprint(result.Farmer),
		}); err != nil {
			return err
		}
		log.Printf("🏃 Writing content to %s file...\n", f.Name())
	}

	w.Flush()

	return nil
}
