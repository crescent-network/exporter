# Exporter

This repository parses exported genesis state at [block height 350670](https://www.mintscan.io/crescent/blocks/350670) to extract data for LUNA and UST holders. 

## Background

$UST peg failure leads to the following revival plan - [Terra Ecosystem Revival Plan 2 AMENDED](https://agora.terra.money/t/terra-ecosystem-revival-plan-2-amended/18498). Terraform Labs are planning to fork the currently running chain and distribute new LUNA coins to LUNA & UST holders based on the token distribution outlined in that proposal. Read the revival plan for more context. Some LUNA and UST holders in Crescent Network couldn't transfer their tokens back to Terra chain due to the fact that IBC channel is closed at some point for security reason. That's why this repository is created to provide snapshot data at certain block height to Terraform Labs so that they allocate new LUNA coins for Crescent LUNA and UST holders. 

## Version

- Crescent [v1.1.0](https://github.com/crescent-network/crescent/releases/tag/v1.1.0)

## Usage

```bash
# Run the program; this will output `result.csv` file
go run main.go
```

## Result File Description

| Column | Type | Description | 
|-------------------|----------|---------------------------------------------------------------------------|
| Address           | string   | The bech32 prefix address for Crescent                                    | 
| LUNA              | sdk.Coin | The total LUNA Coin for an address                                        | 
| UST               | sdk.Coin | The total UST Coin for an address                                         |
| Holder            | bool     | Whether or not the address is holding either LUNA or UST in their balance |
| LiquidityProvider | bool     | Whether or not the address is liquidity provider                          |
| Farmer            | bool     | Whether or not the address is farming staker                              |

