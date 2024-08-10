package config;

import (
	"os"
	"gopkg.in/yaml.v3"
);

type Contract struct {
	Name string `yaml:"name"`;
	AbiPath string `yaml:"abiPath"`;
	Address string `yaml:"address"`;
	Events []string `yaml:"events"`;
	StartBlock int64 `yaml:"startBlock"`;
}

type Config struct {
	Database struct {
		User string `yaml:"username"`;
		DBName string `yaml:"database"`;
		Addr string `yaml:"hostname"`;
		Password string `yaml:"password"`;
	}

	Contracts []Contract `yaml:"contracts"`;

	ChainId int64 `yaml:"chainId"`;

	RpcNodes []string `yaml:"rpcNodes"`;

	MaxWorkers uint `yaml:"maxWorkers"`;

	BlocksPerRequest uint `yaml:"blocksPerRequest"`;
};

func LoadConfig(configFile string) (*Config, error) {
	data, err := os.ReadFile(configFile);

	var config Config;

	if err != nil {
		return nil, err;
	}

	yaml.Unmarshal(data, &config);

	return &config, nil;
}
