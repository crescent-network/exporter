# Exporter

This repository parses exported genesis state at [block height 536806](https://www.mintscan.io/crescent/blocks/536806) to extract data for ATOM holders. ATOM holders at Crescent can be categorized as holders, liquidity providers, and farmers. 

## Resources

- [Tweet about Gnoland airdrop by jaekwon](https://twitter.com/jaekwon/status/1527696556723298306)
- [Airdrop distribution information written in github repository](https://github.com/gnolang/gno/blob/master/gnoland/docs/peace.md#airdrop-distribution)

## Version

- [Crescent core v1.1.0](https://github.com/crescent-network/crescent/releases/tag/v1.1.0)

## Snapshot Block Height

- [Block Height 536806](https://www.mintscan.io/crescent/blocks/536806)

## Usage

```bash
# Uncompress tar file
tar -xvf crescent-exported-536806.tar.gz

# Run the program. This outputs .csv file that contains snapshot data.
go run main.go
```

## Result File Description

| Column | Type | Description | 
|--------------------|----------|-------------------------------------------------------------------------------------|
| address            | string   | The bech32 prefix address for Crescent (CoinType 118)                               | 
| address_gno        | string   | The bech32 prefix address for Gnoland  (CoinType 118)                               | 
| atom               | sdk.Coin | The total amount of ATOM for an address                                             | 
| holder             | bool     | Whether or not the address is holding ATOM in their balance                         |
| liquidity_provider | bool     | Whether or not an account is liquidity provider for pools that correspond with ATOM |
| farmer             | bool     | Whether or not an account is farmer for pools that correspond with ATOM             |

