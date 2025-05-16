#!/usr/bin/env python3

import sys
import requests

response = requests.post(
    "https://polygon-rpc.com/",
    json={
        "jsonrpc": "2.0",
        "method": "eth_blockNumber",
        "params": [],
        "id": 1,
    },
    timeout=10,
)
block_number = int(response.json()["result"], 16)
print(f"block_number: {block_number}")

sys.exit(0 if block_number > 0 else 1)
