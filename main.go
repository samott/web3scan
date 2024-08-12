package main

import (
	"time"
	"sync"
	"math/rand"
	"context"
	"log/slog"
	"math/big"
	"os"

	"database/sql"

	"github.com/go-sql-driver/mysql"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/samott/web3scan/config"
)

var abis map[string]*abi.ABI;

type ScanParams struct {
	contract string;
	startBlock uint64;
	endBlock uint64;
	index uint64;
}

type ScanResult struct {
	events []Event;
	index uint64;
	endBlock uint64;
}

type Event struct {
	args map[string]any;
	contract string;
	event string;
	txHash common.Hash;
}

func scanBlocks(
	contract string,
	rpcUrl string,
	abi *abi.ABI,
	startBlock uint64,
	endBlock uint64,
) ([]Event, error) {
	client, err := ethclient.Dial(rpcUrl);

	if err != nil {
		slog.Error("Failed to connect to RPC node", "error", err);
		return nil, err;
	}

	contractAddress := common.HexToAddress(contract);
	eventSignature := []byte("Transfer(address,address,uint256)");
	eventSignatureHash := crypto.Keccak256Hash(eventSignature);

	startBlockBig := big.NewInt(int64(startBlock));
	endBlockBig := big.NewInt(int64(endBlock));

	query := ethereum.FilterQuery{
		FromBlock: startBlockBig,
		ToBlock:   endBlockBig,
		Addresses: []common.Address{contractAddress},
		Topics:    [][]common.Hash{{eventSignatureHash}},
	}

	logs, err := client.FilterLogs(context.Background(), query)

	if err != nil {
		slog.Error("Error filtering logs", "error", err);
		return nil, err;
	}

	slog.Info("Processing events from block", "count", len(logs));

	events := make([]Event, len(logs));

	for i, log := range logs {
		data := map[string]any{};
		eventHash := log.Topics[0];

		eventAbi, err := abi.EventByID(eventHash);

		if err != nil {
			slog.Error("Error getting event ABI", "error", err);
			return nil, err;
		}

		gotArgs := 0;
		argCount := len(eventAbi.Inputs);

		// Decode the indexed arguments
		for i := 0; i < argCount; i++ {
			if !eventAbi.Inputs[i].Indexed {
				break;
			}

			argName := eventAbi.Inputs[i].Name;
			data[argName] = log.Topics[i+1]; // +1 to skip event hash @ topic 0
			gotArgs++;
		}

		// Decode the remaining arguments
		if gotArgs < argCount {
			unindexed := map[string]any{};

			err = abi.UnpackIntoMap(unindexed, eventAbi.Name, log.Data);

			if err != nil {
				slog.Error("Error getting event ABI", "error", err);
				return nil, err;
			}

			for name, value := range unindexed {
				data[name] = value;
			}
		}

		events[i] = Event{
			args: data,
			contract: contract,
			event: eventAbi.Name,
			txHash: log.TxHash,
		};
		//slog.Info("Event", "event", data);
	}

	return events, nil;
}

func loadAbis(contracts []config.Contract) (map[string]*abi.ABI, error) {
	abis := map[string]*abi.ABI{};
	files := map[string]*abi.ABI{};

	for i := 0; i < len(contracts); i++ {
		path := contracts[i].AbiPath;

		if _, exists := files[path]; exists {
			abis[contracts[i].Address] = files[path];
			continue;
		}

		file, err := os.Open(path);

		if err != nil {
			return abis, err;
		}

		contractAbi, err := abi.JSON(file);

		if err != nil {
			return abis, err;
		}

		abis[contracts[i].Address] = &contractAbi;
		files[path] = &contractAbi;

		file.Close();
	}

	return abis, nil;
}

func handleEvent(events Event) {
	slog.Info("Handling event...");
}

func recordLastBlock(lastBlock uint64) {
	slog.Info("Updating last block...", "lastBlock", lastBlock);
}

func worker(
	wg *sync.WaitGroup,
	in chan ScanParams,
	out chan ScanResult,
	cfg *config.Config,
	workerId uint,
) {
	defer wg.Done();

	for job := range in {
		slog.Info("Start work", "workerId", workerId, "job", job);

		rpc := cfg.RpcNodes[rand.Intn(len(cfg.RpcNodes))];

		events, err := scanBlocks(
			job.contract,
			rpc,
			abis[job.contract],
			job.startBlock,
			job.endBlock,
		);

		if err == nil && len(events) != 0 {
			out <- ScanResult{
				events,
				job.index,
				job.endBlock,
			};
		}

		time.Sleep(time.Second);
	}
}

func main() {
	slog.Info("Running...");

	cfg, err := config.LoadConfig("./web3scan.yaml");

	if err != nil {
		slog.Error("Error loading config file", "error", err);
		return;
	}

	abis, err = loadAbis(cfg.Contracts);

	if err != nil {
		slog.Error("Error loading ABI", "error", err);
		return;
	}

	client, err := ethclient.Dial(cfg.RpcNodes[0]);

	if err != nil {
		slog.Error("Error creating eth client", "error", err);
		return;
	}

	latestBlock, err := client.BlockNumber(context.Background());

	if err != nil {
		slog.Error("Error getting latest block number", "error", err);
		return;
	}

	dbConfig := mysql.Config{
		User: cfg.Database.User,
		DBName: cfg.Database.DBName,
		Addr: cfg.Database.Addr,
		AllowNativePasswords: true,
	};

	db, err := sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		slog.Error("Error connecting to database", "error", err);
		return;
	}

	defer db.Close();

	contract := cfg.Contracts[0];

	lastBlock := uint64(contract.StartBlock) - 1;

	var wg sync.WaitGroup;

	in := make(chan ScanParams);
	out := make(chan ScanResult);

	wg.Add(int(cfg.MaxWorkers));

	for i := range(cfg.MaxWorkers) {
		go worker(&wg, in, out, cfg, i);
	}

	go func() {
		slog.Info("Starting master...");

		var index uint64 = 0;

		defer wg.Done();
		defer close(in);

		for lastBlock < latestBlock {
			var params ScanParams = ScanParams{
				contract.Address,
				lastBlock + 1,
				lastBlock + uint64(cfg.BlocksPerRequest),
				index,
			};

			in <- params;

			lastBlock += uint64(cfg.BlocksPerRequest);
			index++;
		}
	}();

	go func() {
		// XXX: manage multiple contracts

		queue := make(map[uint64]ScanResult);
		next := uint64(0);

		for result := range out {
			queue[result.index] = result;

			runLength := uint64(0);
			endBlock := uint64(0);

			for {
				_, exists := queue[next + runLength];

				if !exists {
					break;
				}

				endBlock = queue[next + runLength].endBlock;
				runLength++;
			}

			for i := uint64(0); i < runLength; i++ {
				res := queue[next + i];

				for j := 0; j < len(res.events); j++ {
					handleEvent(res.events[j]);
				}

				recordLastBlock(endBlock);

				delete(queue, res.index);
			}

			next += runLength;
		}
	}();

	wg.Wait();
}
