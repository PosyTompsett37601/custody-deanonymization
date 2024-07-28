package models

type Config struct {
	EthRPC          string `mapstructure:"EthRPC"`
	PrivateKey      string `mapstructure:"PrivateKey"`
	ContractAddress string `mapstructure:"ContractAddress"`
	WorkerCount     int    `mapstructure:"WorkerCount"`
}
