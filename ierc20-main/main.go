package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"ierc20/models"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/viper"
)

var (
	ethRpc        string //地址
	envPrivateKey string //私钥
	workerCount   int    //并发数基础
	nonce         uint
	successNonce  uint   //成功次数
	GlobalBlock   uint64 //全局区块
	gasPrice      *big.Int
	wg            sync.WaitGroup
	mutex         sync.Mutex

	mintSendCh chan *MintTx
)

type MintTx struct {
	blockNumber uint64
	tx          *types.Transaction
}

// 初始化地址 可以设置默认或者命令行输入
func init() {
	// 设置配置文件的名称和路径
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("configs")

	// 读取配置文件
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("配置文件读取失败： %s", err))
	}
	// 将配置项加载到结构体中
	config := models.Config{
		EthRPC:      viper.GetString("Config.EthRPC"),
		PrivateKey:  viper.GetString("Config.PrivateKey"),
		WorkerCount: viper.GetInt("Config.WorkerCount"),
	}
	//命令行运行 ./ierc20 -privateKey 你的钱包私钥 - -workerCount 协层
	flag.StringVar(&envPrivateKey, "privateKey", config.PrivateKey, "Private key for the Ethereum account")
	flag.IntVar(&workerCount, "workerCount", config.WorkerCount, "Number of concurrent mining workers")
	ethRpc = config.EthRPC
	// fmt.Printf("privateKey:=%v\n", envPrivateKey)
	fmt.Printf("workerCount:=%v\n", workerCount)

}

func main() {
	mintSendCh = make(chan *MintTx, 5)
	client, err := ethclient.Dial(ethRpc)

	if err != nil {
		log.Fatal(err)
	}

	privateKey, err := crypto.HexToECDSA(envPrivateKey)
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)

	if !ok {
		log.Fatal("cannot assert type: publicKey is not of type *ecdsa.PublicKey")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		log.Fatal(err)
	}

	value := big.NewInt(0)
	gasLimit := uint64(28000)
	gasPrice, _ = client.SuggestGasPrice(context.Background())

	if err != nil {
		log.Fatal(err)
	}

	toAddress := common.Address{}

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	DIFF, _ := new(big.Int).SetString("fffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
	nonceRange := uint64(0)

	newBlockNumber, _ := client.BlockNumber(context.Background())
	GlobalBlock = newBlockNumber

	go GetNewBlockNumber(client)

	for i := 0; i < workerCount; i++ {
		// fmt.Println("Init...")
		wg.Add(1)
		go mineWorker(i, &nonceRange, &nonce, gasLimit, toAddress, value, gasPrice, chainID, DIFF, privateKey, client)
	}

	wg.Wait()
}

// 每过12秒 区块+1
func GetNewBlockNumber(client *ethclient.Client) uint64 {
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()

	for {
		sendChannelTx(client)
		select {
		case <-ticker.C:
			sendChannelTx(client)
			mutex.Lock()
			GlobalBlock++
			mutex.Unlock()
			fmt.Println("GlobalBlock: ", GlobalBlock)
		}
	}
}

func sendChannelTx(client *ethclient.Client) {
	select {
	case mintTx := <-mintSendCh:
		if mintTx.blockNumber < GlobalBlock-4 || mintTx.blockNumber > GlobalBlock+4 {
			fmt.Println("mintTx.blockNumber: ", mintTx.blockNumber, "Out of range")
			break
		} else {
			err := client.SendTransaction(context.Background(), mintTx.tx)
			if err != nil {
				fmt.Println("client.SendTransaction Error:", err)
			}
			successNonce++
			fmt.Println("client.SendTransaction: ", mintTx.tx.Hash().Hex(), " BlockNumber: ", mintTx.blockNumber)
			fmt.Println("Success Count: ", successNonce)
			break
		}

	default:
		fmt.Println("channel is empty")
		break
	}
}
func mineWorker(id int, nonceRange, nonce *uint64, gasLimit uint64, toAddress common.Address, value, gasPrice, chainID, DIFF *big.Int, privateKey *ecdsa.PrivateKey, client *ethclient.Client) {
	for {
		mutex.Lock()
		*nonceRange++
		localNonce := new(uint64)
		*localNonce = *nonceRange
		mutex.Unlock()

		fmt.Println("ID: ", id, "localNonce: ", *localNonce)
		var wgg sync.WaitGroup
		for i := *localNonce; i < *localNonce*100000; i++ {
			// fmt.Println("id: ", id, "nonceRange: ", *localNonce, " mintNonce: ", i)
			wgg.Add(1)
			go func() {
				data := fmt.Sprintf("data:application/json,{\"p\":\"ierc-pow\",\"op\":\"mint\",\"tick\":\"ethpi\",\"block\":\"%d\",\"nonce\":\"%d\"}", GlobalBlock, i)
				dataBytes := []byte(data)

				tx := types.NewTx(&types.LegacyTx{
					Nonce:    *nonce,
					To:       &toAddress,
					Value:    value,
					Gas:      gasLimit,
					GasPrice: gasPrice,
					Data:     dataBytes,
				})

				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
				// fmt.Printf("ID: %d Tx Hash : %s\n", id, signedTx.Hash().Hex())
				if err != nil {
					fmt.Println("signedTx Error:", err)
				}

				if signedTx.Hash().Big().Cmp(DIFF) == -1 {
					mutex.Lock()
					fmt.Println("Worker ID: ", id, " nonce: ", *nonce, " mintNonce: ", i)
					println("Worker ID: ", id, " tx: ", signedTx.Hash().Hex())

					*nonce++
					*nonceRange = 0

					gasPrice, _ = client.SuggestGasPrice(context.Background())

					mutex.Unlock()

					mintSendCh <- &MintTx{
						blockNumber: GlobalBlock,
						tx:          signedTx,
					}
				}
			}()
		}
		wgg.Wait()
	}
}

// 8 核 -  256 分钟 400 核 40 // 64
