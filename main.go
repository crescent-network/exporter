package main

import (
	"log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	chain "github.com/crescent-network/crescent/v2/app"
	chaincmd "github.com/crescent-network/crescent/v2/cmd/crescentd/cmd"
	farmingtypes "github.com/crescent-network/crescent/v2/x/farming/types"
	"github.com/crescent-network/crescent/v2/x/liquidity/amm"
	liquiditytypes "github.com/crescent-network/crescent/v2/x/liquidity/types"
)

var (
	genFile    = "crescent-exported-624034.json"
	resultFile = "crescent-exported-624034.csv"

	bankGenState      banktypes.GenesisState
	liquidityGenState liquiditytypes.GenesisState
	farmingGenState   farmingtypes.GenesisState
)

const (
	CREDenom     = "ucre"
	bCREDenom    = "ubcre"
	ATOMDenom    = "ibc/C4CFF46FD6DE35CA4CF4CE031E643C8FDC9BA4B99AE598E9B0ED98FE3A2319F9"
	GRAVDenom    = "ibc/C950356239AD2A205DE09FDF066B1F9FF19A7CA7145EA48A5B19B76EE47E52F7"
	BLDDenom     = "ibc/11F940BCDFD7CFBFD7EDA13F25DA95D308286D441209D780C9863FD4271514EB"
	USDCgrvDenom = "ibc/CD01034D6749F20AAC5330EF4FD8B8CA7C40F7527AB8C4A302FBD2A070852EE1"
	USDCaxlDenom = "ibc/BFF0D3805B50D93E2FA5C0B2DDF7E0B30A631076CD80BC12A48C0E95404B4A41"
	WETHgrvDenom = "ibc/DBF5FA602C46392DE9F4796A0FC7D02F3A8A3D32CA3FAA50B761D4AA6F619E95"
	WETHaxlDenom = "ibc/F1806958CA98757B91C3FA1573ECECD24F6FA3804F074A6977658914A49E65A3"

	Pool1CoinDenom  = "pool1"  // bCRE / CRE (Basic)
	Pool22CoinDenom = "pool22" // bCRE / CRE (Ranged)
	Pool27CoinDenom = "pool27" // bCRE / CRE (Ranged)

	Pool3CoinDenom  = "pool3"  // ATOM / bCRE (Basic)
	Pool26CoinDenom = "pool26" // ATOM / bCRE (Ranged)

	Pool23CoinDenom = "pool23" // BLD / bCRE (Ranged)
	Pool24CoinDenom = "pool24" // BLD / bCRE (Ranged)

	Pool10CoinDenom = "pool10" // GRAV / bCRE (Basic)
	Pool25CoinDenom = "pool25" // GRAV / bCRE (Ranged)

	Pool12CoinDenom = "pool12" // bCRE / USDC.grv (Basic)
	Pool17CoinDenom = "pool17" // bCRE / USDC.axl (Basic)

	Pool14CoinDenom = "pool14" // WETH.grv / bCRE (Basic)
	Pool15CoinDenom = "pool15" // WETH.axl / bCRE (Basic)

	// Include Pool2 and Pool4 ? (bCRE/USTC, LUNC/bCRE)
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
	bCRE              sdk.Coin // the total amount of bCRE for an account
	Holder            bool     // whether or not an account is either CRE or bCRE holder
	LiquidityProvider bool     // whether or not an account is liquidity provider for CRE or bCRE pools
	Farmer            bool     // whether or not an account is farmer for CRE or bCRE pools
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

	// reserveAddressSet saves all reserve accounts that need to be excluded
	// when iterating GetBalances
	reserveAddressSet := map[string]struct{}{} // ReserveAddress => struct
	reserveAddressSet[liquidityGenState.Params.DustCollectorAddress] = struct{}{}
	for _, pool := range liquidityGenState.Pools {
		reserveAddressSet[pool.ReserveAddress] = struct{}{} // pool reserve address
	}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool1CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool22CoinDenom).String()] = struct{}{}
	reserveAddressSet[farmingtypes.StakingReserveAcc(Pool27CoinDenom).String()] = struct{}{}
	// Add more...

	// getbCREAmt is reusable function to return bCRE amount for bCRE holders, lps, and farmers.
	getbCREAmt := func(poolCoin sdk.Coin) (bCREAmt sdk.Int) {
		bCREAmt = sdk.ZeroInt()

		pool := poolByPoolCoinDenom[poolCoin.Denom]
		ammPool := basicPoolByReserveAddress[pool.ReserveAddress]

		amm.Withdraw(rx sdk.Int, ry sdk.Int, ps sdk.Int, pc sdk.Int, feeRate sdk.Dec)

		x, y := ammPool.BasicPool.Withdraw(poolCoin.Amount, sdk.ZeroDec())

		if ammPool.BaseCoin.Denom == bCREDenom {
			bCREAmt = bCREAmt.Add(y)
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
			case bCREDenom:
				if _, ok := resultByAddress[balance.Address]; !ok {
					resultByAddress[balance.Address] = &Result{
						LUNA: sdk.NewInt64Coin(LUNADenom, 0),
						UST:  sdk.NewInt64Coin(USTDenom, 0),
					}
				}
				resultByAddress[balance.Address].LUNA = resultByAddress[balance.Address].LUNA.Add(coin)
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

	// // Parsing farmers from staking records
	// for _, record := range farmingGenState.StakingRecords {
	// 	switch record.StakingCoinDenom {
	// 	case LUNADenom:
	// 		resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(record.Staking.Amount) // NOT FOUND
	// 	case USTDenom:
	// 		resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(record.Staking.Amount) // NOT FOUND
	// 	case Pool2CoinDenom, Pool4CoinDenom, Pool5CoinDenom, Pool6CoinDenom:
	// 		lunaAmt, ustAmt := getLunaUSTAmt(sdk.NewCoin(record.StakingCoinDenom, record.Staking.Amount))
	// 		if _, ok := resultByAddress[record.Farmer]; !ok {
	// 			resultByAddress[record.Farmer] = &Result{
	// 				LUNA: sdk.NewInt64Coin(LUNADenom, 0),
	// 				UST:  sdk.NewInt64Coin(USTDenom, 0),
	// 			}
	// 		}
	// 		resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(ustAmt)
	// 		resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(lunaAmt)
	// 		resultByAddress[record.Farmer].Farmer = true
	// 	}
	// }

	// // Parsing farmers from queued staking records
	// for _, record := range farmingGenState.QueuedStakingRecords {
	// 	switch record.StakingCoinDenom {
	// 	case LUNADenom:
	// 		resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(record.QueuedStaking.Amount) // NOT FOUND
	// 	case USTDenom:
	// 		resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(record.QueuedStaking.Amount) // NOT FOUND
	// 	case Pool2CoinDenom, Pool4CoinDenom, Pool5CoinDenom, Pool6CoinDenom:
	// 		lunaAmt, ustAmt := getLunaUSTAmt(sdk.NewCoin(record.StakingCoinDenom, record.QueuedStaking.Amount))
	// 		if _, ok := resultByAddress[record.Farmer]; !ok {
	// 			resultByAddress[record.Farmer] = &Result{
	// 				LUNA: sdk.NewInt64Coin(LUNADenom, 0),
	// 				UST:  sdk.NewInt64Coin(USTDenom, 0),
	// 			}
	// 		}
	// 		resultByAddress[record.Farmer].UST = resultByAddress[record.Farmer].UST.AddAmount(ustAmt)
	// 		resultByAddress[record.Farmer].LUNA = resultByAddress[record.Farmer].LUNA.AddAmount(lunaAmt)
	// 		resultByAddress[record.Farmer].Farmer = true
	// 	}
	// }

	// // Dump result file
	// if err := dump(resultByAddress); err != nil {
	// 	panic(err)
	// }

	// // Verify total supply of LUNA and UST are the almost the same as results
	// verify(resultByAddress)
}

func verify(resultByAddress map[string]*Result) {
	// totalLUNAAmt, totalUSTAmt := sdk.ZeroInt(), sdk.ZeroInt()
	// for _, result := range resultByAddress {
	// 	totalLUNAAmt = totalLUNAAmt.Add(result.LUNA.Amount)
	// 	totalUSTAmt = totalUSTAmt.Add(result.UST.Amount)
	// }

	// log.Println("[Supply]")
	// log.Println("Total bCRE: ", bankGenState.Supply.AmountOf(LUNADenom))
	// log.Println("Total UST: ", bankGenState.Supply.AmountOf(USTDenom))
	// log.Println("")
	// log.Println("[Result]")
	// log.Println("Total LUNA: ", totalLUNAAmt)
	// log.Println("Total UST: ", totalUSTAmt)
	// log.Println("")
	// log.Println("[Truncation]")
	// log.Println("LUNA Diff: ", bankGenState.Supply.AmountOf(LUNADenom).Sub(totalLUNAAmt))
	// log.Println("UST Diff: ", bankGenState.Supply.AmountOf(USTDenom).Sub(totalUSTAmt))
}

func dump(resultByAddress map[string]*Result) error {
	// f, err := os.OpenFile(resultFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	// if err != nil {
	// 	return err
	// }
	// defer f.Close()

	// w := csv.NewWriter(f)

	// // Write header
	// if err := w.Write([]string{
	// 	"address",
	// 	"address_terra",
	// 	"luna",
	// 	"ust",
	// 	"holder",
	// 	"liquidity_provider",
	// 	"farmer",
	// }); err != nil {
	// 	return fmt.Errorf("failed to write: %w", err)
	// }

	// if err := w.Error(); err != nil {
	// 	return fmt.Errorf("failed to either write or flush: %w", err)
	// }

	// holderNum := 0
	// lpNum := 0
	// farmerNum := 0
	// totalLUNAAmt := sdk.ZeroInt()
	// totalUSTAmt := sdk.ZeroInt()
	// for addr, result := range resultByAddress {
	// 	switch {
	// 	case result.Holder:
	// 		holderNum++
	// 	case result.LiquidityProvider:
	// 		lpNum++
	// 	case result.Farmer:
	// 		farmerNum++
	// 	}

	// 	if result.LUNA.Denom == LUNADenom {
	// 		totalLUNAAmt = totalLUNAAmt.Add(result.LUNA.Amount)
	// 	}
	// 	if result.UST.Denom == USTDenom {
	// 		totalUSTAmt = totalUSTAmt.Add(result.UST.Amount)
	// 	}

	// 	// Convert crescent address to terra
	// 	_, decoded, err := bech32.DecodeAndConvert(addr)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	terraAddr, err := bech32.ConvertAndEncode("terra", decoded)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	if err := w.Write([]string{
	// 		addr,
	// 		terraAddr,
	// 		result.LUNA.String(),
	// 		result.UST.String(),
	// 		fmt.Sprint(result.Holder),
	// 		fmt.Sprint(result.LiquidityProvider),
	// 		fmt.Sprint(result.Farmer),
	// 	}); err != nil {
	// 		return err
	// 	}
	// 	log.Printf("🏃 Writing content to %s file...\n", f.Name())
	// }
	// w.Flush()

	// log.Print("| -----Result------------------------------------------------------")
	// log.Printf("| # of Holders             : %d\n", holderNum)
	// log.Printf("| # of Liquidity Providers : %d\n", lpNum)
	// log.Printf("| # of Farmers             : %d\n", farmerNum)
	// log.Printf("| Total #                  : %d\n", holderNum+lpNum+farmerNum)
	// log.Printf("| Total LUNA Amount        : %s\n", totalLUNAAmt)
	// log.Printf("| Total UST Amount         : %s\n", totalUSTAmt)
	// log.Print("| -----------------------------------------------------------------")

	return nil
}
