# Exporter

This repository parses exported genesis state at specific block height to extract data for Crescent.

## Background

...

## Version

- Crescent [v2.1.1](https://github.com/crescent-network/crescent/releases/tag/v2.1.1)

## Block Heights

- [Block Height xxx](https://www.mintscan.io/crescent/blocks/xxx)

## Usage

```bash
# Uncompress crescent-exported-xxxxxx.json file
tar -xvf crescent-exported-xxxxxx.tar.gz

# Run the program. This outputs .csv file that contains snapshot data.
go run main.go
```

## Result File Description

| Column  | Type   | Description                                           |
| ------- | ------ | ----------------------------------------------------- |
| address | string | The bech32 prefix address for Crescent (CoinType 118) |
| ...     | ...    | ...                                                   |
