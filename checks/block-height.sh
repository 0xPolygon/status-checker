#!/usr/bin/env bash

block_number=$(cast block-number --rpc-url "https://polygon-rpc.com/")
printf "block_number: $block_number"

if (( block_number > 0 )); then
  exit 0
else
  exit 1
fi
