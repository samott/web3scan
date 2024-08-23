package scanner

import (
	"time"
	"sync"
	"math/rand"
	"encoding/json"
	"context"
	"log/slog"
	"math/big"

	"database/sql"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/samott/web3scan/config"
)

type Scanner struct {
	cfg *config.Config;
	db *sql.DB;
	abis map[string]*abi.ABI;
	handleEvent func(event Event, db *sql.Tx) (error);
	in chan ScanParams;
	out chan ScanResult;
	wg sync.WaitGroup;
}

type ScanParams struct {
	contract string;
	startBlock uint64;
	endBlock uint64;
	index uint64;
}

type ScanResult struct {
	contract string;
	events []Event;
	index uint64;
	endBlock uint64;
}

type Event struct {
	args map[string]any;
	contract string;
	event string;
	blockNumber uint64;
	txHash common.Hash;
}

func New(
	cfg *config.Config,
	db *sql.DB,
	abis map[string]*abi.ABI,
	handleEvent func(event Event, db *sql.Tx) (error),
) (*Scanner) {
	return &Scanner{
		cfg,
		db,
		abis,
		handleEvent,
		make(chan ScanParams),
		make(chan ScanResult),
		sync.WaitGroup{},
	};
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
			blockNumber: log.BlockNumber,
			txHash: log.TxHash,
		};
		//slog.Info("Event", "event", data);
	}

	return events, nil;
}

func (s *Scanner) storeEvent(event Event, tx *sql.Tx) (error) {
	data, err := json.Marshal(event.args);

	if err != nil {
		return err;
	}

	_, err = tx.Exec(`
		INSERT INTO events
		(event, contract, blockNumber, txHash, args)
		VALUES
		(?, ?, ?, ?, ?)
	`, event.event, event.contract, event.blockNumber, event.txHash, data);

	return err;
}

func (s *Scanner) worker(
	workerId uint,
) {
	defer s.wg.Done();

	for job := range s.in {
		slog.Info("Start work", "workerId", workerId, "job", job);

		rpc := s.cfg.RpcNodes[rand.Intn(len(s.cfg.RpcNodes))];

		events, err := scanBlocks(
			job.contract,
			rpc,
			s.abis[job.contract],
			job.startBlock,
			job.endBlock,
		);

		if err == nil && len(events) != 0 {
			s.out <- ScanResult{
				job.contract,
				events,
				job.index,
				job.endBlock,
			};
		}

		time.Sleep(time.Second);
	}
}

func (s *Scanner) master(latestBlock uint64) {
	slog.Info("Starting master...");

	var index uint64 = 0;

	defer s.wg.Done();
	defer close(s.in);

	contract := s.cfg.Contracts[0];
	lastBlock := uint64(contract.StartBlock) - 1;

	for lastBlock < latestBlock {
		var params ScanParams = ScanParams{
			contract.Address,
			lastBlock + 1,
			lastBlock + uint64(s.cfg.BlocksPerRequest),
			index,
		};

		s.in <- params;

		lastBlock += uint64(s.cfg.BlocksPerRequest);
		index++;
	}
}

func (s *Scanner) Run() {
	client, err := ethclient.Dial(s.cfg.RpcNodes[0]);

	if err != nil {
		slog.Error("Error creating eth client", "error", err);
		return;
	}

	latestBlock, err := client.BlockNumber(context.Background());

	if err != nil {
		slog.Error("Error getting latest block number", "error", err);
		return;
	}

	s.wg.Add(int(s.cfg.MaxWorkers));

	for i := range(s.cfg.MaxWorkers) {
		go s.worker(i);
	}

	go s.master(latestBlock);

	go func() {
		queues := make(map[string]map[uint64]ScanResult);
		next := uint64(0);

		for result := range s.out {
			if _, exists := queues[result.contract]; !exists {
				queues[result.contract] = make(map[uint64]ScanResult);
			}

			queue := queues[result.contract];
			queue[result.index] = result;

			runLength := uint64(0);
			//endBlock := uint64(0);

			for {
				_, exists := queue[next + runLength];

				if !exists {
					break;
				}

				//endBlock = queue[next + runLength].endBlock;
				runLength++;
			}

			if runLength == 0 {
				continue;
			}

			for i := uint64(0); i < runLength; i++ {
				res := queue[next + i];

				tx, err := s.db.Begin();

				if err != nil {
					slog.Error("Failed to create db tx", "error", err);
					return;
				}

				for j := 0; j < len(res.events); j++ {
					err = s.handleEvent(res.events[j], tx);

					if err != nil {
						break;
					}

					err = s.storeEvent(res.events[j], tx);

					if err != nil {
						break;
					}
				}

				if err == nil {
					tx.Commit();
				} else {
					tx.Rollback();
				}

				delete(queue, res.index);
			}

			next += runLength;
		}
	}();

	s.wg.Wait();
}
