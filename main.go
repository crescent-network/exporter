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
	genFile    = "crescent-exported-350670.json"
	resultFile = "result.csv"

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

	// Collect reserve accounts
	reserveAddressSet := map[string]struct{}{} // ReserveAddress => struct
	reserveAddressSet[liquidityGenState.Params.DustCollectorAddress] = struct{}{}
	for _, pool := range liquidityGenState.Pools {
		reserveAddressSet[pool.ReserveAddress] = struct{}{} // pool reserve address
	}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool2CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool4CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool5CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool6CoinDenom).String()] = struct{}{}

	// resultByAddress stores result information
	resultByAddress := map[string]*Result{} // Address => Result

	// getLunaUSTAmt is reusable function
	getLunaUSTAmt := func(poolCoin sdk.Coin) (lunaAmt, ustAmt sdk.Int) {
		lunaAmt = sdk.ZeroInt()
		ustAmt = sdk.ZeroInt()

		pool := poolByPoolCoinDenom[poolCoin.Denom]
		ammPool := basicPoolByReserveAddress[pool.ReserveAddress]
		x, y := ammPool.BasicPool.Withdraw(poolCoin.Amount, sdk.ZeroDec())

		if ammPool.BaseCoin.Denom == LUNADenom {
			lunaAmt = lunaAmt.Add(y)
		}
		if ammPool.QuoteCoin.Denom == USTDenom {
			ustAmt = ustAmt.Add(x)
		}
		return
	}

	// Parse holders and liquidity providers
	for _, balance := range bankGenState.GetBalances() {
		// Skip pool reserve address
		_, ok := reserveAddressSet[balance.Address]
		if ok {
			continue
		}

		for _, coin := range balance.GetCoins() {
			switch coin.Denom {
			case LUNADenom:
				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				resultByAddress[balance.Address].LUNA = resultByAddress[balance.Address].LUNA.Add(coin)
				resultByAddress[balance.Address].Holder = true

			case USTDenom:
				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				resultByAddress[balance.Address].UST = resultByAddress[balance.Address].UST.Add(coin)
				resultByAddress[balance.Address].Holder = true

			case Pool2CoinDenom, Pool4CoinDenom, Pool5CoinDenom, Pool6CoinDenom:
				lunaAmt, ustAmt := getLunaUSTAmt(coin)

				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				resultByAddress[balance.Address].UST = resultByAddress[balance.Address].UST.AddAmount(ustAmt)
				resultByAddress[balance.Address].LUNA = resultByAddress[balance.Address].LUNA.AddAmount(lunaAmt)
				resultByAddress[balance.Address].LiquidityProvider = true

			default:
				continue
			}
		}
	}

	for _, record := range farmingGenState.StakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(record.Staking.Amount) // NOT FOUND
		case USTDenom:
			resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(record.Staking.Amount) // NOT FOUND
		case Pool2CoinDenom, Pool4CoinDenom, Pool5CoinDenom, Pool6CoinDenom:
			lunaAmt, ustAmt := getLunaUSTAmt(sdk.NewCoin(record.StakingCoinDenom, record.Staking.Amount))
			if _, ok := resultByAddress[record.Farmer]; !ok {
				resultByAddress[record.Farmer] = &Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(ustAmt)
			resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(lunaAmt)
			resultByAddress[record.Farmer].Farmer = true
		}
	}

	for _, record := range farmingGenState.QueuedStakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(record.QueuedStaking.Amount) // NOT FOUND
		case USTDenom:
			resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(record.QueuedStaking.Amount) // NOT FOUND
		case Pool2CoinDenom, Pool4CoinDenom, Pool5CoinDenom, Pool6CoinDenom:
			lunaAmt, ustAmt := getLunaUSTAmt(sdk.NewCoin(record.StakingCoinDenom, record.QueuedStaking.Amount))
			if _, ok := resultByAddress[record.Farmer]; !ok {
				resultByAddress[record.Farmer] = &Result{
					LUNA: sdk.NewInt64Coin(LUNADenom, 0),
					UST:  sdk.NewInt64Coin(USTDenom, 0),
				}
			}
			resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(ustAmt)
			resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(lunaAmt)
			resultByAddress[record.Farmer].Farmer = true
		}
	}

	// Dump result file
	if err := dump(resultByAddress); err != nil {
		panic(err)
	}

	// Debugging code
	totalLUNAAmt, totalUSTAmt := sdk.ZeroInt(), sdk.ZeroInt()
	for _, result := range resultByAddress {
		totalLUNAAmt = totalLUNAAmt.Add(result.LUNA.Amount)
		totalUSTAmt = totalUSTAmt.Add(result.UST.Amount)
	}

	fmt.Println("[Supply]")
	fmt.Println("Total LUNA: ", bankGenState.Supply.AmountOf(LUNADenom))
	fmt.Println("Total UST: ", bankGenState.Supply.AmountOf(USTDenom))
	fmt.Println("")
	fmt.Println("[Result]")
	fmt.Println("Total LUNA: ", totalLUNAAmt)
	fmt.Println("Total UST: ", totalUSTAmt)
	fmt.Println("")
	fmt.Println("LUNA Diff: ", bankGenState.Supply.AmountOf(LUNADenom).Sub(totalLUNAAmt))
	fmt.Println("UST Diff: ", bankGenState.Supply.AmountOf(USTDenom).Sub(totalUSTAmt))
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
		"address_terra",
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

	holderNum := 0
	lpNum := 0
	farmerNum := 0
	totalLUNAAmt := sdk.ZeroInt()
	totalUSTAmt := sdk.ZeroInt()
	for addr, result := range resultByAddress {
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

		// Convert crescent address to terra
		_, decoded, err := bech32.DecodeAndConvert(addr)
		if err != nil {
			return err
		}
		terraAddr, err := bech32.ConvertAndEncode("terra", decoded)
		if err != nil {
			return err
		}

		if err := w.Write([]string{
			addr,
			terraAddr,
			result.LUNA.String(),
			result.UST.String(),
			fmt.Sprint(result.Holder),
			fmt.Sprint(result.LiquidityProvider),
			fmt.Sprint(result.Farmer),
		}); err != nil {
			return err
		}
		log.Printf("🏃 Writing content to %s file...\n", f.Name())
	}
	w.Flush()

	log.Print("| -----Result------------------------------------------------------")
	log.Printf("| # of Holders             : %d\n", holderNum)
	log.Printf("| # of Liquidity Providers : %d\n", lpNum)
	log.Printf("| # of Farmers             : %d\n", farmerNum)
	log.Printf("| Total #                  : %d\n", holderNum+lpNum+farmerNum)
	log.Printf("| Total LUNA Amount        : %s\n", totalLUNAAmt)
	log.Printf("| Total UST Amount         : %s\n", totalUSTAmt)
	log.Print("| -----------------------------------------------------------------")

	return nil
}
