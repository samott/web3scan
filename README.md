# Web3Scan - Ethereum/EVM event monitor

This is a simple tool to find all the events emitted by a contract or
set of contracts, splitting up the work across multiple threads and
(possibly) multiple RPC nodes.

The program will remember the block range it last scanned so it can be
re-run at intervals to continually ingest new events.

A handler function may be implemented to process events as they are
discovered.

Stack: Go.

Licence: BSD-2-Clause.
