# Web3Scan - Ethereum/EVM event monitor [WIP]

This is a simple tool to find all the events emitted by a contract or
set of contracts, splitting up the work across multiple threads and
(possibly) multiple RPC nodes.

The program will remember the block range it last scanned so it can be
re-run at intervals to continually ingest new events.

A handler function may be implemented to process events as they are
discovered.

## Remaining Tasks

The scanner doesn't yet handle block re-orgs and might run into trouble
if an already processed event is seen again in a subsequent block.

Interruption of processing mid-block is not handled properly.

Failure of an RPC request is not handled (the scanner will continue to
queue up data from later blocks and not return to fill in the missing
data.

## About

Stack: Go.

Licence: BSD-2-Clause.
