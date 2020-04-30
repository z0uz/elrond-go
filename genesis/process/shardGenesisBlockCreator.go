package process

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
	"path/filepath"
	"strings"

	logger "github.com/ElrondNetwork/elrond-go-logger"
	"github.com/ElrondNetwork/elrond-go/data"
	"github.com/ElrondNetwork/elrond-go/data/block"
	dataBlock "github.com/ElrondNetwork/elrond-go/data/block"
	"github.com/ElrondNetwork/elrond-go/data/state"
	transactionData "github.com/ElrondNetwork/elrond-go/data/transaction"
	"github.com/ElrondNetwork/elrond-go/genesis"
	"github.com/ElrondNetwork/elrond-go/genesis/process/disabled"
	"github.com/ElrondNetwork/elrond-go/process"
	"github.com/ElrondNetwork/elrond-go/process/block/preprocess"
	"github.com/ElrondNetwork/elrond-go/process/coordinator"
	"github.com/ElrondNetwork/elrond-go/process/factory/shard"
	"github.com/ElrondNetwork/elrond-go/process/rewardTransaction"
	"github.com/ElrondNetwork/elrond-go/process/smartContract"
	"github.com/ElrondNetwork/elrond-go/process/smartContract/builtInFunctions"
	"github.com/ElrondNetwork/elrond-go/process/smartContract/hooks"
	"github.com/ElrondNetwork/elrond-go/process/transaction"
	hardForkProcess "github.com/ElrondNetwork/elrond-go/update/process"
	"github.com/ElrondNetwork/elrond-vm-common"
)

// codeMetadataHex used for initial SC deployment, set to upgrade-able
const codeMetadataHex = "0100"

var log = logger.GetOrCreate("genesis/process")

// CreateShardGenesisBlock will create a shard genesis block
func CreateShardGenesisBlock(arg ArgsGenesisBlockCreator) (data.HeaderHandler, error) {
	processors, err := createProcessorsForShard(arg)
	if err != nil {
		return nil, err
	}

	err = deployInitialSmartContracts(processors, arg)
	if err != nil {
		return nil, err
	}

	rootHash, err := setBalancesToTrie(arg)
	if err != nil {
		return nil, fmt.Errorf("%w encountered when creating genesis block for shard %d",
			err, arg.ShardCoordinator.SelfId())
	}

	//TODO add here delegation process
	if arg.HardForkConfig.MustImport {
		//TODO think about how to integrate when genesis is modified as well for hardfork - when should set balances be called
		// shard genesis probably should stay the same as it was  defined for the actual genesis block - because of transparency
		return createShardGenesisAfterHardFork(arg)
	}

	header := &block.Header{
		Nonce:           0,
		ShardID:         arg.ShardCoordinator.SelfId(),
		BlockBodyType:   block.StateBlock,
		PubKeysBitmap:   []byte{1},
		Signature:       rootHash,
		RootHash:        rootHash,
		PrevRandSeed:    rootHash,
		RandSeed:        rootHash,
		TimeStamp:       arg.GenesisTime,
		AccumulatedFees: big.NewInt(0),
	}

	return header, nil
}

func createShardGenesisAfterHardFork(arg ArgsGenesisBlockCreator) (data.HeaderHandler, error) {
	processors, err := createProcessorsForShard(arg)
	if err != nil {
		return nil, err
	}

	argsPendingTxProcessor := hardForkProcess.ArgsPendingTransactionProcessor{
		Accounts:         arg.Accounts,
		TxProcessor:      processors.txProcessor,
		RwdTxProcessor:   processors.rwdProcessor,
		ScrTxProcessor:   processors.scrProcessor,
		PubKeyConv:       arg.PubkeyConv,
		ShardCoordinator: arg.ShardCoordinator,
	}
	pendingTxProcessor, err := hardForkProcess.NewPendingTransactionProcessor(argsPendingTxProcessor)
	if err != nil {
		return nil, err
	}

	argsShardBlockAfterHardFork := hardForkProcess.ArgsNewShardBlockCreatorAfterHardFork{
		ShardCoordinator:   arg.ShardCoordinator,
		TxCoordinator:      processors.txCoordinator,
		PendingTxProcessor: pendingTxProcessor,
		ImportHandler:      arg.importHandler,
		Marshalizer:        arg.Marshalizer,
		Hasher:             arg.Hasher,
	}
	shardBlockCreator, err := hardForkProcess.NewShardBlockCreatorAfterHardFork(argsShardBlockAfterHardFork)
	if err != nil {
		return nil, err
	}

	hdrHandler, bodyHandler, err := shardBlockCreator.CreateNewBlock(
		arg.ChainID,
		arg.HardForkConfig.StartRound,
		arg.HardForkConfig.StartNonce,
		arg.HardForkConfig.StartEpoch,
	)
	if err != nil {
		return nil, err
	}

	saveGenesisBodyToStorage(processors.txCoordinator, bodyHandler)

	return hdrHandler, nil
}

// setBalancesToTrie adds balances to trie
func setBalancesToTrie(arg ArgsGenesisBlockCreator) (rootHash []byte, err error) {
	if arg.Accounts.JournalLen() != 0 {
		return nil, process.ErrAccountStateDirty
	}

	initialAccounts, err := arg.AccountsParser.InitialAccountsSplitOnAddressesShards(arg.ShardCoordinator)
	if err != nil {
		return nil, err
	}

	initialAccountsForShard := initialAccounts[arg.ShardCoordinator.SelfId()]

	for _, accnt := range initialAccountsForShard {
		err = setBalanceToTrie(arg, accnt)
		if err != nil {
			return nil, err
		}
	}

	rootHash, err = arg.Accounts.Commit()
	if err != nil {
		errToLog := arg.Accounts.RevertToSnapshot(0)
		if errToLog != nil {
			log.Debug("error reverting to snapshot", "error", errToLog.Error())
		}

		return nil, err
	}

	return rootHash, nil
}

func setBalanceToTrie(arg ArgsGenesisBlockCreator, accnt genesis.InitialAccountHandler) error {
	addr, err := arg.PubkeyConv.CreateAddressFromBytes(accnt.AddressBytes())
	if err != nil {
		return fmt.Errorf("%w for address %s", err, accnt.GetAddress())
	}

	accWrp, err := arg.Accounts.LoadAccount(addr)
	if err != nil {
		return err
	}

	account, ok := accWrp.(state.UserAccountHandler)
	if !ok {
		return process.ErrWrongTypeAssertion
	}

	err = account.AddToBalance(accnt.GetBalanceValue())
	if err != nil {
		return err
	}

	return arg.Accounts.SaveAccount(account)
}

func createProcessorsForShard(arg ArgsGenesisBlockCreator) (*genesisProcessors, error) {
	argsParser := vmcommon.NewAtArgumentParser()
	argsBuiltIn := builtInFunctions.ArgsCreateBuiltInFunctionContainer{
		GasMap:          arg.GasMap,
		MapDNSAddresses: make(map[string]struct{}),
	}
	builtInFuncs, err := builtInFunctions.CreateBuiltInFunctionContainer(argsBuiltIn)
	if err != nil {
		return nil, err
	}

	argsHook := hooks.ArgBlockChainHook{
		Accounts:         arg.Accounts,
		PubkeyConv:       arg.PubkeyConv,
		StorageService:   arg.Store,
		BlockChain:       arg.Blkc,
		ShardCoordinator: arg.ShardCoordinator,
		Marshalizer:      arg.Marshalizer,
		Uint64Converter:  arg.Uint64ByteSliceConverter,
		BuiltInFunctions: builtInFuncs,
	}
	vmFactory, err := shard.NewVMContainerFactory(
		arg.VirtualMachineConfig,
		math.MaxUint64,
		arg.GasMap,
		argsHook,
	)
	if err != nil {
		return nil, err
	}

	vmContainer, err := vmFactory.Create()
	if err != nil {
		return nil, err
	}

	interimProcFactory, err := shard.NewIntermediateProcessorsContainerFactory(
		arg.ShardCoordinator,
		arg.Marshalizer,
		arg.Hasher,
		arg.PubkeyConv,
		arg.Store,
		arg.DataPool,
	)
	if err != nil {
		return nil, err
	}

	interimProcContainer, err := interimProcFactory.Create()
	if err != nil {
		return nil, err
	}

	scForwarder, err := interimProcContainer.Get(dataBlock.SmartContractResultBlock)
	if err != nil {
		return nil, err
	}

	receiptTxInterim, err := interimProcContainer.Get(dataBlock.ReceiptBlock)
	if err != nil {
		return nil, err
	}

	badTxInterim, err := interimProcContainer.Get(dataBlock.InvalidBlock)
	if err != nil {
		return nil, err
	}

	gasHandler, err := preprocess.NewGasComputation(arg.Economics)
	if err != nil {
		return nil, err
	}

	argsTxTypeHandler := coordinator.ArgNewTxTypeHandler{
		PubkeyConverter:  arg.PubkeyConv,
		ShardCoordinator: arg.ShardCoordinator,
		BuiltInFuncNames: builtInFuncs.Keys(),
		ArgumentParser:   vmcommon.NewAtArgumentParser(),
	}
	txTypeHandler, err := coordinator.NewTxTypeHandler(argsTxTypeHandler)
	if err != nil {
		return nil, err
	}

	genesisFeeHandler := &disabled.FeeHandler{}
	argsNewScProcessor := smartContract.ArgsNewSmartContractProcessor{
		VmContainer:      vmContainer,
		ArgsParser:       argsParser,
		Hasher:           arg.Hasher,
		Marshalizer:      arg.Marshalizer,
		AccountsDB:       arg.Accounts,
		TempAccounts:     vmFactory.BlockChainHookImpl(),
		PubkeyConv:       arg.PubkeyConv,
		Coordinator:      arg.ShardCoordinator,
		ScrForwarder:     scForwarder,
		TxFeeHandler:     genesisFeeHandler,
		EconomicsFee:     genesisFeeHandler,
		TxTypeHandler:    txTypeHandler,
		GasHandler:       gasHandler,
		BuiltInFunctions: vmFactory.BlockChainHookImpl().GetBuiltInFunctions(),
		TxLogsProcessor:  arg.TxLogsProcessor,
	}
	scProcessor, err := smartContract.NewSmartContractProcessor(argsNewScProcessor)
	if err != nil {
		return nil, err
	}

	rewardsTxProcessor, err := rewardTransaction.NewRewardTxProcessor(
		arg.Accounts,
		arg.PubkeyConv,
		arg.ShardCoordinator,
	)
	if err != nil {
		return nil, err
	}

	transactionProcessor, err := transaction.NewTxProcessor(
		arg.Accounts,
		arg.Hasher,
		arg.PubkeyConv,
		arg.Marshalizer,
		arg.ShardCoordinator,
		scProcessor,
		genesisFeeHandler,
		txTypeHandler,
		genesisFeeHandler,
		receiptTxInterim,
		badTxInterim,
	)
	if err != nil {
		return nil, errors.New("could not create transaction statisticsProcessor: " + err.Error())
	}

	disabledRequestHandler := &disabled.RequestHandler{}
	disabledBlockTracker := &disabled.BlockTracker{}
	disabledBlockSizeComputationHandler := &disabled.BlockSizeComputationHandler{}
	disabledBalanceComputationHandler := &disabled.BalanceComputationHandler{}

	preProcFactory, err := shard.NewPreProcessorsContainerFactory(
		arg.ShardCoordinator,
		arg.Store,
		arg.Marshalizer,
		arg.Hasher,
		arg.DataPool,
		arg.PubkeyConv,
		arg.Accounts,
		disabledRequestHandler,
		transactionProcessor,
		scProcessor,
		scProcessor,
		rewardsTxProcessor,
		arg.Economics,
		gasHandler,
		disabledBlockTracker,
		disabledBlockSizeComputationHandler,
		disabledBalanceComputationHandler,
	)
	if err != nil {
		return nil, err
	}

	preProcContainer, err := preProcFactory.Create()
	if err != nil {
		return nil, err
	}

	txCoordinator, err := coordinator.NewTransactionCoordinator(
		arg.Hasher,
		arg.Marshalizer,
		arg.ShardCoordinator,
		arg.Accounts,
		arg.DataPool.MiniBlocks(),
		disabledRequestHandler,
		preProcContainer,
		interimProcContainer,
		gasHandler,
		genesisFeeHandler,
		disabledBlockSizeComputationHandler,
		disabledBalanceComputationHandler,
	)
	if err != nil {
		return nil, err
	}

	return &genesisProcessors{
		txCoordinator: txCoordinator,
		systemSCs:     nil,
		txProcessor:   transactionProcessor,
		scProcessor:   scProcessor,
		scrProcessor:  scProcessor,
		rwdProcessor:  rewardsTxProcessor,
	}, nil
}

func deployInitialSmartContracts(
	processors *genesisProcessors,
	arg ArgsGenesisBlockCreator,
) error {
	smartContracts, err := arg.SmartContractParser.InitialSmartContractsSplitOnOwnersShards(arg.ShardCoordinator)
	if err != nil {
		return err
	}

	currentShardSmartContracts := smartContracts[arg.ShardCoordinator.SelfId()]

	for _, sc := range currentShardSmartContracts {
		err = deployInitialSmartContract(processors, sc, arg)
		if err != nil {
			return fmt.Errorf("%w for owner %s and filename %s",
				err, sc.GetOwner(), sc.GetFilename())
		}
	}

	return nil
}

func getSCCodeAsHex(filename string) (string, error) {
	code, err := ioutil.ReadFile(filepath.Clean(filename))
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(code), nil
}

func deployInitialSmartContract(
	processors *genesisProcessors,
	sc genesis.InitialSmartContractHandler,
	arg ArgsGenesisBlockCreator,
) error {
	code, err := getSCCodeAsHex(sc.GetFilename())
	if err != nil {
		return err
	}

	vmType := sc.GetVmType()
	deployTxData := strings.Join([]string{code, vmType, codeMetadataHex}, "@")

	nonce, err := getNonce(sc.OwnerBytes(), arg.PubkeyConv, arg.Accounts)
	if err != nil {
		return err
	}

	tx := &transactionData.Transaction{
		Nonce:     nonce,
		SndAddr:   sc.OwnerBytes(),
		Value:     big.NewInt(0),
		RcvAddr:   make([]byte, arg.PubkeyConv.Len()),
		GasPrice:  0,
		GasLimit:  math.MaxUint64,
		Data:      []byte(deployTxData),
		Signature: nil,
	}

	err = processors.txProcessor.ProcessTransaction(tx)
	if err != nil {
		return err
	}

	_, err = arg.Accounts.Commit()
	if err != nil {
		return err
	}

	return nil
}

func getNonce(senderBytes []byte, pubkeyConv state.PubkeyConverter, accounts state.AccountsAdapter) (uint64, error) {
	adr, err := pubkeyConv.CreateAddressFromBytes(senderBytes)
	if err != nil {
		return 0, err
	}

	accnt, err := accounts.LoadAccount(adr)
	if err != nil {
		return 0, err
	}

	return accnt.GetNonce(), nil
}