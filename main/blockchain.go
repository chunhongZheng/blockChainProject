package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/boltdb/bolt"
	"log"
)

type BlockChain struct {
	tip []byte   //最近的一个区块的hash值
	db  *bolt.DB //github.com/boltdb/bolt  数据库
}
type BlockChainIterator struct {
	currentHash []byte
	db          *bolt.DB
}

const dbFile = "blockChain.db"
const blockBucket = "blocks"
const gensisData = "sheShanBlockChain" //创世区块自定义数据
func NewBlockChain(address string) *BlockChain {
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	//	bc := Blockchain{tip, db}
	var isGensisBlock bool
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		if b == nil {
			fmt.Println("区块链不存在，创建一个新的区块链")
			isGensisBlock = true
			transation := NewCoinBaseTx(address, gensisData) //生成矿工交易
			genesis := NewGensisBlock([]*Transation{transation})
			b, err := tx.CreateBucket([]byte(blockBucket))
			if err != nil {
				log.Panic(err)
			}

			err = b.Put(genesis.Hash, genesis.Serialize())
			if err != nil {
				log.Panic(err)
			}
			err = b.Put([]byte("l"), genesis.Hash)
			tip = genesis.Hash
			/*			set:=UTXOSet{&bc}
						set.Reindex()*/
		} else {
			isGensisBlock = false
			tip = b.Get([]byte("l"))
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	bc := BlockChain{tip, db}
	if isGensisBlock == true {
		//创世区块，则初始化UTXO集合
		set := UTXOSet{&bc}
		set.Reindex()
	}
	return &bc
}
func (bc *BlockChain) MineBlock(transactions []*Transation) *Block {
	//验证交易有效与否
	for _, tx := range transactions {
		if bc.VerifyTransaction(tx) != true {
			log.Panic("ERROR:  INVALID transaction")
		} else {
			fmt.Println("transaction verify Success")
		}
	}
	var lastHash []byte
	var lastHeight int32
	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		lastHash = b.Get([]byte("l"))
		blockData := b.Get(lastHash)
		lastBlock := DeserializeBlock(blockData)
		lastHeight = lastBlock.Height
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	newBlock := NewBlock(transactions, lastHash, lastHeight+1)
	err = bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		err := b.Put(newBlock.Hash, newBlock.Serialize())
		if err != nil {
			log.Panic(err)
		}
		err = b.Put([]byte("l"), newBlock.Hash)

		if err != nil {
			log.Panic(err)
		}
		bc.tip = newBlock.Hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	/***更新区块链未花费输出集合**/
	set := UTXOSet{bc}
	set.update(newBlock)
	return newBlock
}
func (bc *BlockChain) iterator() *BlockChainIterator {

	bci := &BlockChainIterator{bc.tip, bc.db}

	return bci
}
func (i *BlockChainIterator) Next() *Block {

	var block *Block

	err := i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		deblock := b.Get(i.currentHash)
		block = DeserializeBlock(deblock)
		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	i.currentHash = block.PrevBlockHash
	return block
}

func (bc *BlockChain) printBlockchain() {
	bci := bc.iterator()
	//fmt.Printf("bci.currenthash:==%x\n",bci.currenthash)

	for {
		block := bci.Next()
		block.String()
		fmt.Println("")
		/*
		         for _,tx:=range block.Transations{
		         	for inId ,in:=range tx.Vin{
		         		fmt.Printf("inId===%d\n",inId)
		         		fmt.Printf("in.Signature==%x\n",in.Signature)
		   			fmt.Printf("上一个交易的id:in.TXId==%x\n",in.TXId)
		         		fmt.Printf("in.VOutIndex===%d\n",in.VOutIndex)
		         		fmt.Printf("in.PubKey %x\n",in.PubKey)
		   		}
		         	for _,out:=range tx.Vout{
		   			fmt.Printf("out.Value==%d\n",out.Value)
		   			fmt.Printf("out.PublicKeyHash %x\n",out.PublicKeyHash)
		   		}
		   	  }
		*/

		//fmt.Printf("长度：%d\n",len(block.PrevBlockHash))
		if len(block.PrevBlockHash) == 0 {
			//跳出循环
			break
		}

	}

}

//未花费交易
func (bc *BlockChain) FindUnSpentTransations(publicKeyHash []byte) []Transation {
	var unspentTXs []Transation        //所有未花费的交易
	spendTXs := make(map[string][]int) // string ----->  []int 存储已经花费的交易
	bci := bc.iterator()
	//遍历区块链
	for {
		block := bci.Next()
		//遍历区块中的交易列表  开始
		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)
			//遍历交易中的每项输出
		output:
			for outIdx, out := range tx.Vout {
				if spendTXs[txID] != nil {
					//当前输出为被花费输出
					//遍历已花费交易集合，判断当前输出是否已经为花费的
					for _, spentOut := range spendTXs[txID] {
						if spentOut == outIdx {
							continue output
						}
					}
				}
				if out.CanBeUnlockedWith(publicKeyHash) {
					//如果该输出是指定address交易，则添加进去列表，得到指定address未花费交易集合
					unspentTXs = append(unspentTXs, *tx)
				}
			}
			//将输入变成已花费交易
			if tx.IsCoinBase() == false {
				//非矿工交易
				for _, in := range tx.Vin {
					txID := hex.EncodeToString(in.TXId)
					if in.CanUnlockOutputWith(publicKeyHash) {
						spendTXs[txID] = append(spendTXs[txID], in.VOutIndex)
					}
				}
			}
		} ////遍历区块中的交易列表  结束
		if len(block.PrevBlockHash) == 0 {
			//遍历到创世区块，表明已经处理完所有区块，跳出当前循环
			//	fmt.Println("遍历到创世区块，表明已经处理完所有区块，跳出当前循环")
			break
		}
	}
	return unspentTXs
}
func (bc *BlockChain) FindUTXO(publickeyHash []byte) []TXOutput {
	var UTXOs []TXOutput
	unspendTransaction := bc.FindUnSpentTransations(publickeyHash)
	for _, tx := range unspendTransaction {
		for _, out := range tx.Vout {
			if out.CanBeUnlockedWith(publickeyHash) {
				UTXOs = append(UTXOs, out)
			}

		}
	}
	return UTXOs
}

func (bc *BlockChain) FindSpendableOutputs(publickeyHash []byte, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	unspentTXs := bc.FindUnSpentTransations(publickeyHash)
	accumulated := 0
Work:
	for _, tx := range unspentTXs {
		txID := hex.EncodeToString(tx.ID)

		for outIdx, out := range tx.Vout {
			if out.CanBeUnlockedWith(publickeyHash) && accumulated < amount {
				accumulated += out.Value
				unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)

				if accumulated >= amount {
					//当前未花费输出已足够，直接跳出循环
					break Work
				}
			}
		}
	}
	fmt.Println(len(unspentOutputs))
	return accumulated, unspentOutputs

}

/***用私钥对交易进行签名*/
func (bc *BlockChain) SignTransation(tx *Transation, privateKey *ecdsa.PrivateKey) {
	perTXs := make(map[string]Transation)

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransactionById(vin.TXId)
		if err != nil {
			log.Panic(err)
		}
		perTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	tx.Sign(*privateKey, perTXs) //对交易进行签名
}

func (bc *BlockChain) FindTransactionById(ID []byte) (Transation, error) {

	bci := bc.iterator()
	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			if bytes.Compare(tx.ID, ID) == 0 {
				return *tx, nil
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return Transation{}, errors.New("transation is not found")
}

/***验证交易中的每笔输入的引用*/
func (bc *BlockChain) VerifyTransaction(tx *Transation) bool {
	prevTXs := make(map[string]Transation)
	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransactionById(vin.TXId)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	return tx.Verify(prevTXs)
}

func (bc *BlockChain) FindAllUTXO() map[string]TXOutputs {
	UTXO := make(map[string]TXOutputs)
	spendTXs := make(map[string][]int) // string ----->  []int 存储已经花费的交易

	bci := bc.iterator()
	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			txId := hex.EncodeToString(tx.ID)
		output:
			for outIdx, out := range tx.Vout {
				for _, spendOutIds := range spendTXs[txId] {
					if spendOutIds == outIdx {
						//该笔输出已经花费
						continue output
					}
				}
				outs := UTXO[txId]
				outs.Outputs = append(outs.Outputs, out)
				UTXO[txId] = outs
			}
			if tx.IsCoinBase() == false {
				for _, in := range tx.Vin {
					inTxId := hex.EncodeToString(in.TXId)
					spendTXs[inTxId] = append(spendTXs[inTxId], in.VOutIndex) //存储已花费交易id   交易id->引用输出的序号
				}
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return UTXO

}

func (bc *BlockChain) GetBestHeight() int32 {
	var lastBlock Block
	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		lastHash := b.Get([]byte("l"))
		blockData := b.Get(lastHash)
		lastBlock = *DeserializeBlock(blockData)
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return lastBlock.Height
}

//获取区块hash
func (bc *BlockChain) GetLockHash() [][]byte {
	var blocks [][]byte
	bci := bc.iterator()
	for {
		block := bci.Next()
		blocks = append(blocks, block.Hash)
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return blocks
}

func (bc *BlockChain) GetBlock(blockHash []byte) (Block, error) {
	var block Block
	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		blockData := b.Get(blockHash)
		if blockData == nil {
			return errors.New("Block is not Found")
		}
		block = *DeserializeBlock(blockData)
		return nil
	})
	if err != nil {
		return block, err
	}
	return block, nil
}

func (bc *BlockChain) AddBlock(block *Block) {

	err := bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		blockInDb := b.Get(block.Hash)
		if blockInDb != nil {
			return nil
		}
		blockData := block.Serialize()
		err := b.Put(block.Hash, blockData)
		if err != nil {
			log.Panic(err)
		}
		lastHash := b.Get([]byte("l"))
		lastBlockData := b.Get(lastHash)
		lastBlock := DeserializeBlock(lastBlockData)
		if block.Height > lastBlock.Height {
			//当前区块高度大于当前链的最高高度，更新其信息
			err = b.Put([]byte("l"), block.Hash)
			if err != nil {
				log.Panic(err)
			}
			bc.tip = block.Hash
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}

}
