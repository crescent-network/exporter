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
	lunaTotalSupply := bankGenState.GetSupply().AmountOf(LUNADenom)
	ustTotalSupply := bankGenState.GetSupply().AmountOf(USTDenom)
	pool2TotalSupply := bankGenState.GetSupply().AmountOf(Pool2CoinDenom)
	pool4TotalSupply := bankGenState.GetSupply().AmountOf(Pool4CoinDenom)
	pool5TotalSupply := bankGenState.GetSupply().AmountOf(Pool5CoinDenom)
	pool6TotalSupply := bankGenState.GetSupply().AmountOf(Pool6CoinDenom)

	// pairIdByPair is a cache to store all pairs
	pairIdByPair := map[uint64]liquiditytypes.Pair{} // PairId => Pair
	for _, pair := range liquidityGenState.Pairs {
		pairIdByPair[pair.Id] = pair
	}

	// poolCoinDenomByPool is a cache to store all pools
	poolCoinDenomByPool := map[string]liquiditytypes.Pool{} // PoolCoinDenom => Pool
	for _, pool := range liquidityGenState.Pools {
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

	lunaCal := sdk.ZeroInt()
	ustCal := sdk.ZeroInt()
	pool2Cal := sdk.ZeroInt()
	pool4Cal := sdk.ZeroInt()
	pool5Cal := sdk.ZeroInt()
	pool6Cal := sdk.ZeroInt()

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
				lunaCal = lunaCal.Add(coin.Amount)
			case USTDenom:
				ustCal = ustCal.Add(coin.Amount)
			case Pool2CoinDenom: // bCRE / UST
				pool2Cal = pool2Cal.Add(coin.Amount)
			case Pool4CoinDenom: // LUNA / bCRE
				pool4Cal = pool4Cal.Add(coin.Amount)
			case Pool5CoinDenom: // ATOM / UST
				pool5Cal = pool5Cal.Add(coin.Amount)
			case Pool6CoinDenom: // LUNA / UST
				pool6Cal = pool6Cal.Add(coin.Amount)
			default:
				continue
			}
		}
	}

	for _, record := range farmingGenState.StakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			lunaCal = lunaCal.Add(record.Staking.Amount)
		case USTDenom:
			ustCal = ustCal.Add(record.Staking.Amount)
		case Pool2CoinDenom:
			pool2Cal = pool2Cal.Add(record.Staking.Amount)
		case Pool4CoinDenom:
			pool4Cal = pool4Cal.Add(record.Staking.Amount)
		case Pool5CoinDenom:
			pool5Cal = pool5Cal.Add(record.Staking.Amount)
		case Pool6CoinDenom:
			pool6Cal = pool6Cal.Add(record.Staking.Amount)
		}
	}

	for _, record := range farmingGenState.QueuedStakingRecords {
		switch record.StakingCoinDenom {
		case LUNADenom:
			lunaCal = lunaCal.Add(record.QueuedStaking.Amount)
		case USTDenom:
			ustCal = ustCal.Add(record.QueuedStaking.Amount)
		case Pool2CoinDenom:
			pool2Cal = pool2Cal.Add(record.QueuedStaking.Amount)
		case Pool4CoinDenom:
			pool4Cal = pool4Cal.Add(record.QueuedStaking.Amount)
		case Pool5CoinDenom:
			pool5Cal = pool5Cal.Add(record.QueuedStaking.Amount)
		case Pool6CoinDenom:
			pool6Cal = pool6Cal.Add(record.QueuedStaking.Amount)
		}
	}

	/*
		[Supply]
		LUNA:  78527370715
		UST:  9715351474339
		Pool2:  2346766210770531073
		Pool4:  28254290233531039
		Pool5:  122877799840887088
		Pool6:  32018139414600667

		[Result]
		LUNA:  78527370715
		UST:  9715351474339
		Pool2:  2346766210770531073
		Pool4:  28254290233531039
		Pool5:  122877799840887088
		Pool6:  32018139414600667
	*/

	fmt.Println("[Supply]")
	fmt.Println("LUNA: ", lunaTotalSupply)
	fmt.Println("UST: ", ustTotalSupply)
	fmt.Println("Pool2: ", pool2TotalSupply)
	fmt.Println("Pool4: ", pool4TotalSupply)
	fmt.Println("Pool5: ", pool5TotalSupply)
	fmt.Println("Pool6: ", pool6TotalSupply)
	fmt.Println("")
	fmt.Println("[Result]")
	fmt.Println("LUNA: ", lunaCal)
	fmt.Println("UST: ", ustCal)
	fmt.Println("Pool2: ", pool2Cal)
	fmt.Println("Pool4: ", pool4Cal)
	fmt.Println("Pool5: ", pool5Cal)
	fmt.Println("Pool6: ", pool6Cal)

}

func dump(addrByResult map[string]Result) error {
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
