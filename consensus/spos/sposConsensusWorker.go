package spos

import (
	"bytes"
	"fmt"

	"github.com/ElrondNetwork/elrond-go-sandbox/chronology"
	"github.com/ElrondNetwork/elrond-go-sandbox/data/block"
	"github.com/ElrondNetwork/elrond-go-sandbox/data/blockchain"
	"github.com/ElrondNetwork/elrond-go-sandbox/hashing"
	"github.com/ElrondNetwork/elrond-go-sandbox/logger"
	"github.com/ElrondNetwork/elrond-go-sandbox/marshal"
	"github.com/ElrondNetwork/elrond-go-sandbox/process"
	"github.com/ElrondNetwork/elrond-go-sandbox/crypto"
)

var log = logger.NewDefaultLogger()

//TODO: Split in multiple structs, with Single Responsibility

const (
	// SrStartRound defines ID of subround "Start round"
	SrStartRound chronology.SubroundId = iota
	// SrBlock defines ID of subround "block"
	SrBlock
	// SrCommitmentHash defines ID of subround "commitment hash"
	SrCommitmentHash
	// SrBitmap defines ID of subround "bitmap"
	SrBitmap
	// SrCommitment defines ID of subround "commitment"
	SrCommitment
	// SrSignature defines ID of subround "signature"
	SrSignature
	// SrEndRound defines ID of subround "End round"
	SrEndRound
)

//TODO: current shards (this should be injected, and this const should be removed later)
const shardId = 0

//TODO: maximum transactions in one block (this should be injected, and this const should be removed later)
const maxTransactionsInBlock = 1000

// MessageType specifies what type of message was received
type MessageType int

const (
	// MtBlockBody defines ID of a message that has a block body inside
	MtBlockBody MessageType = iota
	// MtBlockHeader defines ID of a message that has a block header inside
	MtBlockHeader
	// MtCommitmentHash defines ID of a message that has a commitment hash inside
	MtCommitmentHash
	// MtBitmap defines ID of a message that has a bitmap inside
	MtBitmap
	// MtCommitment defines ID of a message that has a commitment inside
	MtCommitment
	// MtSignature defines ID of a message that has a Signature inside
	MtSignature
	// MtUnknown defines ID of a message that has an unknown BlHeaderHash inside
	MtUnknown
)

// ConsensusData defines the data needed by spos to comunicate between nodes over network in all subrounds
type ConsensusData struct {
	BlHeaderHash []byte
	SubRoundData []byte
	PubKey       []byte
	Signature    []byte
	MsgType      MessageType
	TimeStamp    uint64
}

// NewConsensusData creates a new ConsensusData object
func NewConsensusData(
	blHeaderHash []byte,
	subRoundData []byte,
	pubKey []byte,
	sig []byte,
	msg MessageType,
	tms uint64,
) *ConsensusData {

	return &ConsensusData{
		BlHeaderHash: blHeaderHash,
		SubRoundData: subRoundData,
		PubKey:       pubKey,
		Signature:    sig,
		MsgType:      msg,
		TimeStamp:    tms,
	}
}

// SPOSConsensusWorker defines the data needed by spos to comunicate between nodes which are in the validators group
type SPOSConsensusWorker struct {
	Cns             *Consensus
	Hdr             *block.Header
	Blk             *block.TxBlockBody
	Blkc            *blockchain.BlockChain
	Rounds          int // only for statistic
	RoundsWithBlock int // only for statistic
	BlockProcessor  process.BlockProcessor
	ChRcvMsg        map[MessageType]chan *ConsensusData
	hasher          hashing.Hasher
	marshalizer     marshal.Marshalizer
	keyGen          crypto.KeyGenerator
	privKey         crypto.PrivateKey
	pubKey          crypto.PublicKey
	multiSigner     crypto.MultiSigner
	// this is a pointer to a function which actually send the message from a node to the network
	OnSendMessage        func([]byte)
	OnBroadcastHeader    func([]byte)
	OnBroadcastBlockBody func([]byte)
}

// NewConsensusWorker creates a new SPOSConsensusWorker object
func NewConsensusWorker(
	cns *Consensus,
	blkc *blockchain.BlockChain,
	hasher hashing.Hasher,
	marshalizer marshal.Marshalizer,
	blockProcessor process.BlockProcessor,
	multisig crypto.MultiSigner,
	keyGen crypto.KeyGenerator,
	privKey crypto.PrivateKey,
	pubKey crypto.PublicKey,
) (*SPOSConsensusWorker, error) {

	err := checkNewConsensusWorkerParams(
		cns,
		blkc,
		hasher,
		marshalizer,
		blockProcessor,
		multisig,
		keyGen,
		privKey,
		pubKey,
	)

	if err != nil {
		return nil, err
	}

	sposWorker := SPOSConsensusWorker{
		Cns:            cns,
		Blkc:           blkc,
		hasher:         hasher,
		marshalizer:    marshalizer,
		BlockProcessor: blockProcessor,
		multiSigner:    multisig,
		keyGen:         keyGen,
		privKey:        privKey,
		pubKey:         pubKey,
	}

	sposWorker.ChRcvMsg = make(map[MessageType]chan *ConsensusData)

	nodes := 0

	if cns != nil &&
		cns.RoundConsensus != nil {
		nodes = len(cns.RoundConsensus.ConsensusGroup())
	}

	sposWorker.ChRcvMsg[MtBlockBody] = make(chan *ConsensusData, nodes)
	sposWorker.ChRcvMsg[MtBlockHeader] = make(chan *ConsensusData, nodes)
	sposWorker.ChRcvMsg[MtCommitmentHash] = make(chan *ConsensusData, nodes)
	sposWorker.ChRcvMsg[MtBitmap] = make(chan *ConsensusData, nodes)
	sposWorker.ChRcvMsg[MtCommitment] = make(chan *ConsensusData, nodes)
	sposWorker.ChRcvMsg[MtSignature] = make(chan *ConsensusData, nodes)

	go sposWorker.CheckChannels()

	return &sposWorker, nil
}

func checkNewConsensusWorkerParams(
	cns *Consensus,
	blkc *blockchain.BlockChain,
	hasher hashing.Hasher,
	marshalizer marshal.Marshalizer,
	blockProcessor process.BlockProcessor,
	multisig crypto.MultiSigner,
	keyGen crypto.KeyGenerator,
	privKey crypto.PrivateKey,
	pubKey crypto.PublicKey,
) error {
	if cns == nil {
		return ErrNilConsensus
	}

	if blkc == nil {
		return ErrNilBlockChain
	}

	if hasher == nil {
		return ErrNilHasher
	}

	if marshalizer == nil {
		return ErrNilMarshalizer
	}

	if blockProcessor == nil {
		return ErrNilBlockProcessor
	}

	if multisig == nil {
		return ErrNilMultiSigner
	}

	if keyGen == nil {
		return ErrNilKeyGenerator
	}

	if privKey == nil {
		return ErrNilPrivateKey
	}

	if pubKey == nil {
		return ErrNilPublicKey
	}

	return nil
}

// DoStartRoundJob method is the function which actually do the job of the StartRound subround
// (it is used as the handler function of the doSubroundJob pointer variable function in Subround struct,
// from spos package)
func (sposWorker *SPOSConsensusWorker) DoStartRoundJob() bool {
	sposWorker.Blk = nil
	sposWorker.Hdr = nil
	sposWorker.Cns.Data = nil
	sposWorker.Cns.ResetRoundStatus()
	sposWorker.Cns.ResetRoundState()

	leader, err := sposWorker.Cns.GetLeader()

	if err != nil {
		log.Error(err.Error())
		leader = "Unknown"
	}

	if leader == sposWorker.Cns.SelfPubKey() {
		leader = fmt.Sprintf(leader + " (MY TURN)")
	}

	log.Info("Step 0: Preparing for this round with leader %s ", leader)

	pubKeys := sposWorker.Cns.ConsensusGroup()

	selfIndex, err := sposWorker.Cns.IndexSelfConsensusGroup()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	sposWorker.multiSigner, err = sposWorker.multiSigner.NewMultiSiger(sposWorker.hasher, pubKeys, sposWorker.privKey, uint16(selfIndex))

	return true
}

// DoEndRoundJob method is the function which actually do the job of the EndRound subround
// (it is used as the handler function of the doSubroundJob pointer variable function in Subround struct,
// from spos package)
func (sposWorker *SPOSConsensusWorker) DoEndRoundJob() bool {
	if !sposWorker.Cns.CheckEndRoundConsensus() {
		return false
	}

	// Aggregate sig and add it to the block
	sig, err := sposWorker.multiSigner.AggregateSigs()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	sposWorker.Hdr.Signature = sig

	// Commit the block (commits also the account state)
	err = sposWorker.BlockProcessor.CommitBlock(sposWorker.Blkc, sposWorker.Hdr, sposWorker.Blk)

	if err != nil {
		log.Error(err.Error())
		sposWorker.BlockProcessor.RevertAccountState()
		return false
	}

	err = sposWorker.BlockProcessor.RemoveBlockTxsFromPool(sposWorker.Blk)

	if err != nil {
		log.Error(err.Error())
	}

	// broadcast block body
	err = sposWorker.broadcastTxBlockBody()
	if err != nil {
		log.Error(err.Error())
	}

	// broadcast header
	err = sposWorker.broadcastHeader()
	if err != nil {
		log.Error(err.Error())
	}

	if sposWorker.Cns.IsNodeLeaderInCurrentRound(sposWorker.Cns.SelfPubKey()) {
		log.Info(">>>>>>>>>>>>>>>>>>>> ADDED PROPOSED BLOCK WITH NONCE  %d  IN BLOCKCHAIN "+
			"<<<<<<<<<<<<<<<<<<<<\n", sposWorker.Hdr.Nonce)
	} else {
		log.Info(">>>>>>>>>>>>>>>>>>>> ADDED SYNCHRONIZED BLOCK WITH NONCE  %d  IN BLOCKCHAIN "+
			"<<<<<<<<<<<<<<<<<<<<\n", sposWorker.Hdr.Nonce)
	}

	sposWorker.Rounds++          // only for statistic
	sposWorker.RoundsWithBlock++ // only for statistic

	return true
}

// DoBlockJob method actually send the proposed block in the Block subround, when this node is leader
// (it is used as a handler function of the doSubroundJob pointer function declared in Subround struct,
// from spos package)
func (sposWorker *SPOSConsensusWorker) DoBlockJob() bool {
	if sposWorker.ShouldSynch() { // if node is not synchronized yet, it has to continue the bootstrapping mechanism
		log.Info("Canceled round %d in subround %s",
			sposWorker.Cns.Chr.Round().Index(), sposWorker.Cns.GetSubroundName(SrBlock))
		sposWorker.Cns.Chr.SetSelfSubround(-1)
		return false
	}

	if sposWorker.Cns.Status(SrBlock) == SsFinished || // is subround Block already finished?
		sposWorker.Cns.GetJobDone(sposWorker.Cns.SelfPubKey(), SrBlock) || // has been block already sent?
		!sposWorker.Cns.IsNodeLeaderInCurrentRound(sposWorker.Cns.SelfPubKey()) { // is another node leader in this round?
		return false
	}

	if !sposWorker.SendBlockBody() ||
		!sposWorker.SendBlockHeader() {
		return false
	}

	sposWorker.Cns.SetJobDone(sposWorker.Cns.SelfPubKey(), SrBlock, true)

	return true
}

// SendBlockBody method send the proposed block body in the Block subround
func (sposWorker *SPOSConsensusWorker) SendBlockBody() bool {

	currentSubRound := sposWorker.GetSubround()

	haveTime := func() bool {
		if sposWorker.GetSubround() > currentSubRound {
			return false
		}
		return true
	}

	blk, err := sposWorker.BlockProcessor.CreateTxBlockBody(
		shardId,
		maxTransactionsInBlock,
		sposWorker.Cns.Chr.Round().Index(),
		haveTime,
	)

	blkStr, err := sposWorker.marshalizer.Marshal(blk)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	dta := NewConsensusData(
		nil,
		blkStr,
		[]byte(sposWorker.Cns.selfPubKey),
		nil,
		MtBlockBody,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 1: Sending block body")

	sposWorker.Blk = blk

	return true
}

// GetSubround method returns current subround taking in consideration the current time
func (sposWorker *SPOSConsensusWorker) GetSubround() chronology.SubroundId {
	return sposWorker.Cns.Chr.GetSubroundFromDateTime(sposWorker.Cns.Chr.SyncTime().CurrentTime(sposWorker.Cns.Chr.ClockOffset()))
}

// SendBlockHeader method send the proposed block header in the Block subround
func (sposWorker *SPOSConsensusWorker) SendBlockHeader() bool {
	hdr := &block.Header{}

	if sposWorker.Blkc.CurrentBlockHeader == nil {
		hdr.Nonce = 1
		hdr.Round = uint32(sposWorker.Cns.Chr.Round().Index())
		hdr.TimeStamp = sposWorker.GetTime()
	} else {
		hdr.Nonce = sposWorker.Blkc.CurrentBlockHeader.Nonce + 1
		hdr.Round = uint32(sposWorker.Cns.Chr.Round().Index())
		hdr.TimeStamp = sposWorker.GetTime()

		prevHeader, err := sposWorker.marshalizer.Marshal(sposWorker.Blkc.CurrentBlockHeader)

		if err != nil {
			log.Error(err.Error())
			return false
		}
		hdr.PrevHash = prevHeader
	}

	blkStr, err := sposWorker.marshalizer.Marshal(sposWorker.Blk)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	hdr.BlockBodyHash = sposWorker.hasher.Compute(string(blkStr))

	hdrStr, err := sposWorker.marshalizer.Marshal(hdr)

	hdrHash := sposWorker.hasher.Compute(string(hdrStr))

	if err != nil {
		log.Error(err.Error())
		return false
	}

	dta := NewConsensusData(
		hdrHash,
		hdrStr,
		[]byte(sposWorker.Cns.SelfPubKey()),
		nil,
		MtBlockHeader,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 1: Sending block header")

	sposWorker.Hdr = hdr
	sposWorker.Cns.Data = hdrHash

	return true
}

func (sposWorker *SPOSConsensusWorker) genCommitmentHash() ([]byte, error) {
	commitmentSecret, commitment, err := sposWorker.multiSigner.CreateCommitment()

	if err != nil {
		return nil, err
	}

	selfIndex, err := sposWorker.Cns.IndexSelfConsensusGroup()

	if err != nil {
		return nil, err
	}

	err = sposWorker.multiSigner.AddCommitment(uint16(selfIndex), commitment)

	if err != nil {
		return nil, err
	}

	err = sposWorker.multiSigner.SetCommitmentSecret(commitmentSecret)

	if err != nil {
		return nil, err
	}

	commitmentHash := sposWorker.hasher.Compute(string(commitment))

	err = sposWorker.multiSigner.AddCommitmentHash(uint16(selfIndex), commitmentHash)

	if err != nil {
		return nil, err
	}

	return commitmentHash, nil
}

// DoCommitmentHashJob method is the function which is actually used to send the commitment hash for the received
// block from the leader in the CommitmentHash subround (it is used as the handler function of the doSubroundJob
// pointer variable function in Subround struct, from spos package)
func (sposWorker *SPOSConsensusWorker) DoCommitmentHashJob() bool {
	if sposWorker.Cns.Status(SrBlock) != SsFinished { // is subround Block not finished?
		return sposWorker.DoBlockJob()
	}

	if sposWorker.Cns.Status(SrCommitmentHash) == SsFinished || // is subround CommitmentHash already finished?
		sposWorker.Cns.GetJobDone(sposWorker.Cns.SelfPubKey(), SrCommitmentHash) || // is commitment hash already sent?
		sposWorker.Cns.Data == nil { // is consensus data not set?
		return false
	}

	commitmentHash, err := sposWorker.genCommitmentHash()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	dta := NewConsensusData(
		sposWorker.Cns.Data,
		commitmentHash,
		[]byte(sposWorker.Cns.SelfPubKey()),
		nil,
		MtCommitmentHash,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 2: Sending commitment hash")

	sposWorker.Cns.SetJobDone(sposWorker.Cns.SelfPubKey(), SrCommitmentHash, true)

	return true
}

func (sposWorker *SPOSConsensusWorker) genBitmap(subround chronology.SubroundId) []byte {
	// generate bitmap according to set commitment hashes
	l := len(sposWorker.Cns.ConsensusGroup())

	bitmap := make([]byte, l/8+1)

	for i := 0; i < l; i++ {
		if sposWorker.Cns.GetJobDone(sposWorker.Cns.ConsensusGroup()[i], subround) {
			bitmap[i/8] = 1 << (uint16(i) % 8)
		}
	}

	return bitmap
}

// DoBitmapJob method is the function which is actually used to send the bitmap with the commitment hashes
// received, in the Bitmap subround, when this node is leader (it is used as the handler function of the
// doSubroundJob pointer variable function in Subround struct, from spos package)
func (sposWorker *SPOSConsensusWorker) DoBitmapJob() bool {
	if sposWorker.Cns.Status(SrCommitmentHash) != SsFinished { // is subround CommitmentHash not finished?
		return sposWorker.DoCommitmentHashJob()
	}

	if sposWorker.Cns.Status(SrBitmap) == SsFinished || // is subround Bitmap already finished?
		sposWorker.Cns.GetJobDone(sposWorker.Cns.SelfPubKey(), SrBitmap) || // has been bitmap already sent?
		!sposWorker.Cns.IsNodeLeaderInCurrentRound(sposWorker.Cns.SelfPubKey()) || // is another node leader in this round?
		sposWorker.Cns.Data == nil { // is consensus data not set?
		return false
	}

	bitmap := sposWorker.genBitmap(SrCommitmentHash)

	dta := NewConsensusData(
		sposWorker.Cns.Data,
		bitmap,
		[]byte(sposWorker.Cns.SelfPubKey()),
		nil,
		MtBitmap,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 3: Sending bitmap")

	for i := 0; i < len(sposWorker.Cns.ConsensusGroup()); i++ {
		if sposWorker.Cns.GetJobDone(sposWorker.Cns.ConsensusGroup()[i], SrCommitmentHash) {
			sposWorker.Cns.SetJobDone(sposWorker.Cns.ConsensusGroup()[i], SrBitmap, true)
		}
	}

	return true
}

// DoCommitmentJob method is the function which is actually used to send the commitment for the received block,
// in the Commitment subround (it is used as the handler function of the doSubroundJob pointer variable function
// in Subround struct, from spos package)
func (sposWorker *SPOSConsensusWorker) DoCommitmentJob() bool {
	if sposWorker.Cns.Status(SrBitmap) != SsFinished { // is subround Bitmap not finished?
		return sposWorker.DoBitmapJob()
	}

	if sposWorker.Cns.Status(SrCommitment) == SsFinished || // is subround Commitment already finished?
		sposWorker.Cns.GetJobDone(sposWorker.Cns.SelfPubKey(), SrCommitment) || // has been commitment already sent?
		!sposWorker.Cns.IsValidatorInBitmap(sposWorker.Cns.SelfPubKey()) || // isn't node in the leader's bitmap?
		sposWorker.Cns.Data == nil { // is consensus data not set?
		return false
	}

	selfIndex, err := sposWorker.Cns.IndexSelfConsensusGroup()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	// commitment
	commitment, err := sposWorker.multiSigner.Commitment(uint16(selfIndex))

	if err != nil {
		log.Error(err.Error())
		return false
	}

	dta := NewConsensusData(
		sposWorker.Cns.Data,
		commitment,
		[]byte(sposWorker.Cns.SelfPubKey()),
		nil,
		MtCommitment,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 4: Sending commitment")

	sposWorker.Cns.SetJobDone(sposWorker.Cns.SelfPubKey(), SrCommitment, true)

	return true
}

// DoSignatureJob method is the function which is actually used to send the Signature for the received block,
// in the Signature subround (it is used as the handler function of the doSubroundJob pointer variable function
// in Subround struct, from spos package)
func (sposWorker *SPOSConsensusWorker) DoSignatureJob() bool {
	if sposWorker.Cns.Status(SrCommitment) != SsFinished { // is subround Commitment not finished?
		return sposWorker.DoCommitmentJob()
	}

	if sposWorker.Cns.Status(SrSignature) == SsFinished || // is subround Signature already finished?
		sposWorker.Cns.GetJobDone(sposWorker.Cns.SelfPubKey(), SrSignature) || // has been signature already sent?
		!sposWorker.Cns.IsValidatorInBitmap(sposWorker.Cns.SelfPubKey()) || // isn't node in the leader's bitmap?
		sposWorker.Cns.Data == nil { // is consensus data not set?
		return false
	}

	// first compute commitment aggregation
	_, err := sposWorker.multiSigner.AggregateCommitments()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	sigPart, err := sposWorker.multiSigner.SignPartial()

	if err != nil {
		log.Error(err.Error())
		return false
	}

	dta := NewConsensusData(
		sposWorker.Cns.Data,
		sigPart,
		[]byte(sposWorker.Cns.SelfPubKey()),
		nil,
		MtSignature,
		sposWorker.GetTime())

	if !sposWorker.SendConsensusMessage(dta) {
		return false
	}

	log.Info("Step 5: Sending signature")

	sposWorker.Cns.SetJobDone(sposWorker.Cns.SelfPubKey(), SrSignature, true)

	return true
}

func (sposWorker *SPOSConsensusWorker) genSignedConsensusData(cnsDta *ConsensusData) ([]byte, error) {

	message, err := sposWorker.marshalizer.Marshal(cnsDta)

	if err != nil {
		return nil, err
	}

	// sign message
	cnsDta.Signature, err = sposWorker.privKey.Sign(message)

	if err != nil {
		return nil, err
	}

	// marshal message with signature
	return sposWorker.marshalizer.Marshal(cnsDta)

}

// SendConsensusMessage method send the message to the nodes which are in the validators group
func (sposWorker *SPOSConsensusWorker) SendConsensusMessage(cnsDta *ConsensusData) bool {
	// marshal message to sign
	message, err := sposWorker.genSignedConsensusData(cnsDta)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	// send message
	if sposWorker.OnSendMessage == nil {
		log.Error("OnSendMessage call back function is not set")
		return false
	}

	go sposWorker.OnSendMessage(message)

	return true
}

func (sposWorker *SPOSConsensusWorker) broadcastTxBlockBody() error {
	if sposWorker.Blk != nil {
		return ErrNilTxBlockBody
	}

	message, err := sposWorker.marshalizer.Marshal(sposWorker.Blk)

	if err != nil {
		return err
	}

	// send message
	if sposWorker.OnBroadcastBlockBody == nil {
		return ErrNilOnBroadcastTxBlockBody
	}

	go sposWorker.OnBroadcastBlockBody(message)

	return nil
}

// SendConsensusMessage method send the message to the nodes which are in the validators group
func (sposWorker *SPOSConsensusWorker) broadcastHeader() error {
	if sposWorker.Hdr == nil {
		return ErrNilBlockHeader
	}

	message, err := sposWorker.marshalizer.Marshal(sposWorker.Hdr)

	if err != nil {
		return err
	}

	// send message
	if sposWorker.OnBroadcastHeader == nil {
		return ErrNilOnBroadcastHeader
	}

	go sposWorker.OnBroadcastHeader(message)

	return nil
}

// ExtendBlock method put this subround in the extended mode and print some messages
func (sposWorker *SPOSConsensusWorker) ExtendBlock() {
	sposWorker.Cns.SetStatus(SrBlock, SsExtended)
	log.Info("Step 1: Extended the <BLOCK> subround")
}

// ExtendCommitmentHash method put this subround in the extended mode and print some messages
func (sposWorker *SPOSConsensusWorker) ExtendCommitmentHash() {
	sposWorker.Cns.SetStatus(SrCommitmentHash, SsExtended)
	if sposWorker.Cns.ComputeSize(SrCommitmentHash) < sposWorker.Cns.Threshold(SrCommitmentHash) {
		log.Info("Step 2: Extended the <COMMITMENT_HASH> subround. Got only %d from %d commitment hashes"+
			" which are not enough", sposWorker.Cns.ComputeSize(SrCommitmentHash), len(sposWorker.Cns.ConsensusGroup()))
	} else {
		log.Info("Step 2: Extended the <COMMITMENT_HASH> subround")
	}
}

// ExtendBitmap method put this subround in the extended mode and print some messages
func (sposWorker *SPOSConsensusWorker) ExtendBitmap() {
	sposWorker.Cns.SetStatus(SrBitmap, SsExtended)
	log.Info("Step 3: Extended the <BITMAP> subround")
}

// ExtendCommitment method put this subround in the extended mode and print some messages
func (sposWorker *SPOSConsensusWorker) ExtendCommitment() {
	sposWorker.Cns.SetStatus(SrCommitment, SsExtended)
	log.Info("Step 4: Extended the <COMMITMENT> subround. Got only %d from %d commitments"+
		" which are not enough", sposWorker.Cns.ComputeSize(SrCommitment), len(sposWorker.Cns.ConsensusGroup()))
}

// ExtendSignature method put this subround in the extended mode and print some messages
func (sposWorker *SPOSConsensusWorker) ExtendSignature() {
	sposWorker.Cns.SetStatus(SrSignature, SsExtended)
	log.Info("Step 5: Extended the <SIGNATURE> subround. Got only %d from %d sigantures"+
		" which are not enough", sposWorker.Cns.ComputeSize(SrSignature), len(sposWorker.Cns.ConsensusGroup()))
}

// ExtendEndRound method just print some messages as no extend will be permited, because a new round
// will be start
func (sposWorker *SPOSConsensusWorker) ExtendEndRound() {
	log.Info(">>>>>>>>>>>>>>>>>>>> THIS ROUND NO BLOCK WAS ADDED TO THE BLOCKCHAIN <<<<<<<<<<<<<<<<<<<<\n")
	sposWorker.Rounds++ // only for statistic
}

// ReceivedMessage method redirects the received message to the channel which should handle it
func (sposWorker *SPOSConsensusWorker) ReceivedMessage(cnsData *ConsensusData) {
	sigOK := sposWorker.checkSignature(cnsData)

	if sigOK != nil {
		return
	}

	if ch, ok := sposWorker.ChRcvMsg[cnsData.MsgType]; ok {
		ch <- cnsData
	}
}

// CheckChannels method is used to listen to the channels through which node receives and consumes,
// during the round, different messages from the nodes which are in the validators group
func (sposWorker *SPOSConsensusWorker) CheckChannels() {
	for {
		select {
		case rcvDta := <-sposWorker.ChRcvMsg[MtBlockBody]:
			sposWorker.ReceivedBlockBody(rcvDta)
		case rcvDta := <-sposWorker.ChRcvMsg[MtBlockHeader]:
			sposWorker.ReceivedBlockHeader(rcvDta)
		case rcvDta := <-sposWorker.ChRcvMsg[MtCommitmentHash]:
			sposWorker.ReceivedCommitmentHash(rcvDta)
		case rcvDta := <-sposWorker.ChRcvMsg[MtBitmap]:
			sposWorker.ReceivedBitmap(rcvDta)
		case rcvDta := <-sposWorker.ChRcvMsg[MtCommitment]:
			sposWorker.ReceivedCommitment(rcvDta)
		case rcvDta := <-sposWorker.ChRcvMsg[MtSignature]:
			sposWorker.ReceivedSignature(rcvDta)
		}
	}
}

func (sposWorker *SPOSConsensusWorker) checkSignature(cnsData *ConsensusData) error {
	if cnsData == nil {
		return ErrNilConsensusData
	}

	if cnsData.PubKey == nil {
		return ErrNilPublicKey
	}

	if cnsData.Signature == nil {
		return ErrNilSignature
	}

	pubKey, err := sposWorker.keyGen.PublicKeyFromByteArray(cnsData.PubKey)

	if err != nil {
		return err
	}

	dataNoSig := *cnsData
	signature := cnsData.Signature

	dataNoSig.Signature = nil
	dataNoSigString, err := sposWorker.marshalizer.Marshal(dataNoSig)

	if err != nil {
		return err
	}

	_, err = pubKey.Verify(dataNoSigString, signature)

	return err
}

// ReceivedBlockBody method is called when a block body is received through the block body channel.
func (sposWorker *SPOSConsensusWorker) ReceivedBlockBody(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is block body received from myself?
		!sposWorker.Cns.IsNodeLeaderInCurrentRound(node) || // is another node leader in this round?
		sposWorker.Blk != nil { // is block body already received?
		return false
	}

	log.Info("Step 1: Received block body")

	sposWorker.Blk = sposWorker.DecodeBlockBody(&cnsDta.SubRoundData)

	if sposWorker.Blk != nil &&
		sposWorker.Hdr != nil {
		err := sposWorker.BlockProcessor.ProcessBlock(sposWorker.Blkc, sposWorker.Hdr, sposWorker.Blk)

		if err != nil {
			log.Error(err.Error())
			return false
		}

		sposWorker.multiSigner.SetMessage(sposWorker.Cns.Data)
		sposWorker.Cns.RoundConsensus.SetJobDone(node, SrBlock, true)
	}

	return true
}

// DecodeBlockBody method decodes block body which is marshalized in the received message
func (sposWorker *SPOSConsensusWorker) DecodeBlockBody(dta *[]byte) *block.TxBlockBody {
	if dta == nil {
		return nil
	}

	var blk block.TxBlockBody

	err := sposWorker.marshalizer.Unmarshal(&blk, *dta)

	if err != nil {
		log.Error(err.Error())
		return nil
	}

	return &blk
}

// ReceivedBlockHeader method is called when a block header is received through the block header channel.
// If the block header is valid, than the validatorRoundStates map coresponding to the node which sent it,
// is set on true for the subround Block
func (sposWorker *SPOSConsensusWorker) ReceivedBlockHeader(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is block header received from myself?
		sposWorker.Cns.Status(SrBlock) == SsFinished || // is subround Block already finished?
		!sposWorker.Cns.IsNodeLeaderInCurrentRound(node) || // is another node leader in this round?
		sposWorker.Cns.RoundConsensus.GetJobDone(node, SrBlock) { // is block header already received?
		return false
	}

	hdr := sposWorker.DecodeBlockHeader(&cnsDta.SubRoundData)

	if !sposWorker.CheckIfBlockIsValid(hdr) {
		log.Info("Canceled round %d in subround %s",
			sposWorker.Cns.Chr.Round().Index(), sposWorker.Cns.GetSubroundName(SrBlock))
		sposWorker.Cns.Chr.SetSelfSubround(-1)
		return false
	}

	log.Info("Step 1: Received block header")

	sposWorker.Hdr = hdr
	sposWorker.Cns.Data = cnsDta.BlHeaderHash

	if sposWorker.Blk != nil &&
		sposWorker.Hdr != nil {
		err := sposWorker.BlockProcessor.ProcessBlock(sposWorker.Blkc, sposWorker.Hdr, sposWorker.Blk)

		if err != nil {
			log.Error(err.Error())
			return false
		}

		sposWorker.multiSigner.SetMessage(sposWorker.Cns.Data)
		sposWorker.Cns.RoundConsensus.SetJobDone(node, SrBlock, true)
	}

	return true
}

// DecodeBlockHeader method decodes block header which is marshalized in the received message
func (sposWorker *SPOSConsensusWorker) DecodeBlockHeader(dta *[]byte) *block.Header {
	if dta == nil {
		return nil
	}

	var hdr block.Header

	err := sposWorker.marshalizer.Unmarshal(&hdr, *dta)

	if err != nil {
		log.Error(err.Error())
		return nil
	}

	return &hdr
}

// ReceivedCommitmentHash method is called when a commitment hash is received through the commitment hash
// channel. If the commitment hash is valid, than the jobDone map coresponding to the node which sent it,
// is set on true for the subround ComitmentHash
func (sposWorker *SPOSConsensusWorker) ReceivedCommitmentHash(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is commitment hash received from myself?
		sposWorker.Cns.Status(SrCommitmentHash) == SsFinished || // is subround CommitmentHash already finished?
		!sposWorker.Cns.IsNodeInConsensusGroup(node) || // isn't node in the jobDone group?
		sposWorker.Cns.RoundConsensus.GetJobDone(node, SrCommitmentHash) || // is commitment hash already received?
		sposWorker.Cns.Data == nil || // is consensus data not set?
		!bytes.Equal(cnsDta.BlHeaderHash, sposWorker.Cns.Data) { // is this the consesnus data of this round?
		return false
	}

	// if this node is leader in this round and already he received 2/3 + 1 of commitment hashes
	// he will ignore any others received later
	if sposWorker.Cns.IsNodeLeaderInCurrentRound(sposWorker.Cns.SelfPubKey()) {
		if sposWorker.Cns.IsCommitmentHashReceived(sposWorker.Cns.Threshold(SrCommitmentHash)) {
			return false
		}
	}

	index, err := sposWorker.Cns.ConsensusGroupIndex(node)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	err = sposWorker.multiSigner.AddCommitmentHash(uint16(index), cnsDta.SubRoundData)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	sposWorker.Cns.RoundConsensus.SetJobDone(node, SrCommitmentHash, true)
	return true
}

func countBitmapFlags(bitmap []byte) uint16 {
	nbBytes := len(bitmap)
	flags := 0
	for i := 0; i < nbBytes; i++ {
		for j := 0; j < 8; j++ {
			if bitmap[i]&(1<<uint8(j)) != 0 {
				flags++
			}
		}
	}
	return uint16(flags)
}

// ReceivedBitmap method is called when a bitmap is received through the bitmap channel.
// If the bitmap is valid, than the jobDone map coresponding to the node which sent it,
// is set on true for the subround Bitmap
func (sposWorker *SPOSConsensusWorker) ReceivedBitmap(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is bitmap received from myself?
		sposWorker.Cns.Status(SrBitmap) == SsFinished || // is subround Bitmap already finished?
		!sposWorker.Cns.IsNodeLeaderInCurrentRound(node) || // is another node leader in this round?
		sposWorker.Cns.RoundConsensus.GetJobDone(node, SrBitmap) || // is bitmap already received?
		sposWorker.Cns.Data == nil || // is consensus data not set?
		!bytes.Equal(cnsDta.BlHeaderHash, sposWorker.Cns.Data) { // is this the consesnus data of this round?
		return false
	}

	signersBitmap := cnsDta.SubRoundData

	// count signers
	nbSigners := countBitmapFlags(signersBitmap)

	if int(nbSigners) < sposWorker.Cns.Threshold(SrBitmap) {
		log.Info("Canceled round %d in subround %s",
			sposWorker.Cns.Chr.Round().Index(), sposWorker.Cns.GetSubroundName(SrBitmap))
		sposWorker.Cns.Chr.SetSelfSubround(-1)
		return false
	}

	publicKeys := sposWorker.Cns.ConsensusGroup()

	for i := 0; i < len(publicKeys); i++ {
		byteNb := i / 8
		bitNb := i % 8
		isNodeSigner := (signersBitmap[byteNb] & (1 << uint8(bitNb))) != 0

		if isNodeSigner {
			sposWorker.Cns.RoundConsensus.SetJobDone(publicKeys[i], SrBitmap, true)
		}
	}

	return true
}

// ReceivedCommitment method is called when a commitment is received through the commitment channel.
// If the commitment is valid, than the jobDone map coresponding to the node which sent it,
// is set on true for the subround Comitment
func (sposWorker *SPOSConsensusWorker) ReceivedCommitment(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is commitment received from myself?
		sposWorker.Cns.Status(SrCommitment) == SsFinished || // is subround Commitment already finished?
		!sposWorker.Cns.IsValidatorInBitmap(node) || // isn't node in the bitmap group?
		sposWorker.Cns.RoundConsensus.GetJobDone(node, SrCommitment) || // is commitment already received?
		sposWorker.Cns.Data == nil || // is consensus data not set?
		!bytes.Equal(cnsDta.BlHeaderHash, sposWorker.Cns.Data) { // is this the consesnus data of this round?
		return false
	}

	index, err := sposWorker.Cns.ConsensusGroupIndex(node)

	if err != nil {
		log.Info(err.Error())
		return false
	}

	computedCommitmentHash := sposWorker.hasher.Compute(string(cnsDta.SubRoundData))
	rcvCommitmentHash, err := sposWorker.multiSigner.CommitmentHash(uint16(index))

	if !bytes.Equal(computedCommitmentHash, rcvCommitmentHash) {
		log.Info("commitment does not match expected val", computedCommitmentHash, rcvCommitmentHash)
		return false
	}

	err = sposWorker.multiSigner.AddCommitment(uint16(index), cnsDta.SubRoundData)

	if err != nil {
		return false
	}

	sposWorker.Cns.RoundConsensus.SetJobDone(node, SrCommitment, true)
	return true
}

// ReceivedSignature method is called when a Signature is received through the Signature channel.
// If the Signature is valid, than the jobDone map coresponding to the node which sent it,
// is set on true for the subround Signature
func (sposWorker *SPOSConsensusWorker) ReceivedSignature(cnsDta *ConsensusData) bool {
	node := string(cnsDta.PubKey)

	if node == sposWorker.Cns.SelfPubKey() || // is signature received from myself?
		sposWorker.Cns.Status(SrSignature) == SsFinished || // is subround Signature already finished?
		!sposWorker.Cns.IsValidatorInBitmap(node) || // isn't node in the bitmap group?
		sposWorker.Cns.RoundConsensus.GetJobDone(node, SrSignature) || // is signature already received?
		sposWorker.Cns.Data == nil || // is consensus data not set?
		!bytes.Equal(cnsDta.BlHeaderHash, sposWorker.Cns.Data) { // is this the consesnus data of this round?
		return false
	}

	index, err := sposWorker.Cns.ConsensusGroupIndex(node)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	// verify partial signature
	err = sposWorker.multiSigner.VerifyPartial(uint16(index), cnsDta.SubRoundData)

	if err != nil {
		log.Error(err.Error())
		return false
	}

	err = sposWorker.multiSigner.AddSignPartial(uint16(index), cnsDta.SubRoundData)

	if err != nil {
		return false
	}

	sposWorker.Cns.RoundConsensus.SetJobDone(node, SrSignature, true)
	return true
}

// CheckIfBlockIsValid method checks if the received block is valid
func (sposWorker *SPOSConsensusWorker) CheckIfBlockIsValid(receivedHeader *block.Header) bool {
	// TODO: This logic is temporary and it should be refactored after the bootstrap mechanism
	// TODO: will be implemented

	if sposWorker.Blkc.CurrentBlockHeader == nil {
		if receivedHeader.Nonce == 1 { // first block after genesis
			if bytes.Equal(receivedHeader.PrevHash, []byte("")) {
				return true
			}

			log.Info("Hash not match: local block hash is empty and node received block "+
				"with previous hash %s", receivedHeader.PrevHash)
			return false
		}

		// to resolve the situation when a node comes later in the network and it has the
		// bootstrap mechanism not implemented yet (he will accept the block received)
		log.Info("Nonce not match: local block nonce is 0 and node received block "+
			"with nonce %d", receivedHeader.Nonce)
		log.Info(">>>>>>>>>>>>>>>>>>>> ACCEPTED BLOCK WITH NONCE %d BECAUSE BOOSTRAP IS NOT "+
			"IMPLEMENTED YET <<<<<<<<<<<<<<<<<<<<\n", receivedHeader.Nonce)
		return true
	}

	if receivedHeader.Nonce < sposWorker.Blkc.CurrentBlockHeader.Nonce+1 {
		log.Info("Nonce not match: local block nonce is %d and node received block "+
			"with nonce %d", sposWorker.Blkc.CurrentBlockHeader.Nonce, receivedHeader.Nonce)
		return false
	}

	if receivedHeader.Nonce == sposWorker.Blkc.CurrentBlockHeader.Nonce+1 {
		if bytes.Equal(receivedHeader.PrevHash, sposWorker.Blkc.CurrentBlockHeader.BlockBodyHash) {
			return true
		}

		log.Info("Hash not match: local block hash is %s and node received block "+
			"with previous hash %s", sposWorker.Blkc.CurrentBlockHeader.BlockBodyHash, receivedHeader.PrevHash)
		return false
	}

	// to resolve the situation when a node misses some Blocks and it has the bootstrap mechanism
	// not implemented yet (he will accept the block received)
	log.Info("Nonce not match: local block nonce is %d and node received block "+
		"with nonce %d", sposWorker.Blkc.CurrentBlockHeader.Nonce, receivedHeader.Nonce)
	log.Info(">>>>>>>>>>>>>>>>>>>> ACCEPTED BLOCK WITH NONCE %d BECAUSE BOOSTRAP IS NOT "+
		"IMPLEMENTED YET <<<<<<<<<<<<<<<<<<<<\n", receivedHeader.Nonce)
	return true
}

// ShouldSynch method returns the synch state of the node. If it returns 'true', this means that the node
// is not synchronized yet and it has to continue the bootstrapping mechanism, otherwise the node is already
// synched and it can participate to the consensus, if it is in the jobDone group of this round
func (sposWorker *SPOSConsensusWorker) ShouldSynch() bool {
	if sposWorker.Cns == nil ||
		sposWorker.Cns.Chr == nil ||
		sposWorker.Cns.Chr.Round() == nil {
		return true
	}

	rnd := sposWorker.Cns.Chr.Round()

	if sposWorker.Blkc == nil ||
		sposWorker.Blkc.CurrentBlockHeader == nil {
		return rnd.Index() > 0
	}

	return sposWorker.Blkc.CurrentBlockHeader.Round+1 < uint32(rnd.Index())
}

// GetMessageTypeName method returns the name of the message from a given message ID
func (sposWorker *SPOSConsensusWorker) GetMessageTypeName(messageType MessageType) string {
	switch messageType {
	case MtBlockBody:
		return "<BLOCK_BODY>"
	case MtBlockHeader:
		return "<BLOCK_HEADER>"
	case MtCommitmentHash:
		return "<COMMITMENT_HASH>"
	case MtBitmap:
		return "<BITMAP>"
	case MtCommitment:
		return "<COMMITMENT>"
	case MtSignature:
		return "<SIGNATURE>"
	case MtUnknown:
		return "<UNKNOWN>"
	default:
		return "Undifined message type"
	}
}

// GetFormatedTime method returns a string containing the formated current time
func (sposWorker *SPOSConsensusWorker) GetFormatedTime() string {
	return sposWorker.Cns.Chr.SyncTime().FormatedCurrentTime(sposWorker.Cns.Chr.ClockOffset())
}

// GetTime method returns a string containing the current time
func (sposWorker *SPOSConsensusWorker) GetTime() uint64 {
	return uint64(sposWorker.Cns.Chr.SyncTime().CurrentTime(sposWorker.Cns.Chr.ClockOffset()).Unix())
}

// CheckEndRoundConsensus method checks if the consensus is achieved in each subround from first subround to the given
// subround. If the consensus is achieved in one subround, the subround status is marked as finished
func (cns *Consensus) CheckEndRoundConsensus() bool {
	for i := SrBlock; i <= SrSignature; i++ {
		currentSubRound := cns.Chr.SubroundHandlers()[i]
		if !currentSubRound.Check() {
			return false
		}
	}

	return true
}

// CheckStartRoundConsensus method checks if the consensus is achieved in the start subround.
func (cns *Consensus) CheckStartRoundConsensus() bool {
	return true
}

// CheckBlockConsensus method checks if the consensus in the <BLOCK> subround is achieved
func (cns *Consensus) CheckBlockConsensus() bool {
	if cns.Status(SrBlock) == SsFinished {
		return true
	}

	if cns.IsBlockReceived(cns.Threshold(SrBlock)) {
		cns.PrintBlockCM() // only for printing block consensus messages
		cns.SetStatus(SrBlock, SsFinished)
		return true
	}

	return false
}

// CheckCommitmentHashConsensus method checks if the consensus in the <COMMITMENT_HASH> subround is achieved
func (cns *Consensus) CheckCommitmentHashConsensus() bool {
	if cns.Status(SrCommitmentHash) == SsFinished {
		return true
	}

	threshold := cns.Threshold(SrCommitmentHash)

	if !cns.IsNodeLeaderInCurrentRound(cns.selfPubKey) {
		threshold = len(cns.consensusGroup)
	}

	if cns.IsCommitmentHashReceived(threshold) {
		cns.PrintCommitmentHashCM() // only for printing commitment hash consensus messages
		cns.SetStatus(SrCommitmentHash, SsFinished)
		return true
	}

	if cns.CommitmentHashesCollected(cns.Threshold(SrBitmap)) {
		cns.PrintCommitmentHashCM() // only for printing commitment hash consensus messages
		cns.SetStatus(SrCommitmentHash, SsFinished)
		return true
	}

	return false
}

// CheckBitmapConsensus method checks if the consensus in the <BITMAP> subround is achieved
func (cns *Consensus) CheckBitmapConsensus() bool {
	if cns.Status(SrBitmap) == SsFinished {
		return true
	}

	if cns.CommitmentHashesCollected(cns.Threshold(SrBitmap)) {
		cns.PrintBitmapCM() // only for printing bitmap consensus messages
		cns.SetStatus(SrBitmap, SsFinished)
		return true
	}

	return false
}

// CheckCommitmentConsensus method checks if the consensus in the <COMMITMENT> subround is achieved
func (cns *Consensus) CheckCommitmentConsensus() bool {
	if cns.Status(SrCommitment) == SsFinished {
		return true
	}

	if cns.CommitmentsCollected(cns.Threshold(SrCommitment)) {
		cns.PrintCommitmentCM() // only for printing commitment consensus messages
		cns.SetStatus(SrCommitment, SsFinished)
		return true
	}

	return false
}

// CheckSignatureConsensus method checks if the consensus in the <SIGNATURE> subround is achieved
func (cns *Consensus) CheckSignatureConsensus() bool {
	if cns.Status(SrSignature) == SsFinished {
		return true
	}

	if cns.SignaturesCollected(cns.Threshold(SrSignature)) {
		cns.PrintSignatureCM() // only for printing signature consensus messages
		cns.SetStatus(SrSignature, SsFinished)
		return true
	}

	return false
}

// GetSubroundName returns the name of each subround from a given subround ID
func (cns *Consensus) GetSubroundName(subroundId chronology.SubroundId) string {
	switch subroundId {
	case SrStartRound:
		return "<START_ROUND>"
	case SrBlock:
		return "<BLOCK>"
	case SrCommitmentHash:
		return "<COMMITMENT_HASH>"
	case SrBitmap:
		return "<BITMAP>"
	case SrCommitment:
		return "<COMMITMENT>"
	case SrSignature:
		return "<SIGNATURE>"
	case SrEndRound:
		return "<END_ROUND>"
	default:
		return "Undifined subround"
	}
}

// PrintBlockCM method prints the <BLOCK> consensus messages
func (cns *Consensus) PrintBlockCM() {
	if !cns.IsNodeLeaderInCurrentRound(cns.selfPubKey) {
		log.Info("Step 1: Synchronized block")
	}
	log.Info("Step 1: SubroundId <BLOCK> has been finished")
}

// PrintCommitmentHashCM method prints the <COMMITMENT_HASH> consensus messages
func (cns *Consensus) PrintCommitmentHashCM() {
	n := cns.ComputeSize(SrCommitmentHash)
	if n == len(cns.consensusGroup) {
		log.Info("Step 2: Received all (%d from %d) commitment hashes", n, len(cns.consensusGroup))
	} else {
		log.Info("Step 2: Received %d from %d commitment hashes, which are enough", n, len(cns.consensusGroup))
	}
	log.Info("Step 2: SubroundId <COMMITMENT_HASH> has been finished")
}

// PrintBitmapCM method prints the <BITMAP> consensus messages
func (cns *Consensus) PrintBitmapCM() {
	if !cns.IsNodeLeaderInCurrentRound(cns.selfPubKey) {
		msg := fmt.Sprintf(cns.Chr.SyncTime().FormatedCurrentTime(cns.Chr.ClockOffset())+
			"Step 3: Received bitmap from leader, matching with my own, and it got %d from %d commitment hashes, which are enough",
			cns.ComputeSize(SrBitmap), len(cns.consensusGroup))

		if cns.IsValidatorInBitmap(cns.selfPubKey) {
			msg = fmt.Sprintf(msg+"%s", ", AND I WAS selected in this bitmap")
		} else {
			msg = fmt.Sprintf(msg+"%s", ", BUT I WAS NOT selected in this bitmap")
		}

		log.Info(msg)
	}
	log.Info("Step 3: SubroundId <BITMAP> has been finished")
}

// PrintCommitmentCM method prints the <COMMITMENT> consensus messages
func (cns *Consensus) PrintCommitmentCM() {
	log.Info("Step 4: Received %d from %d commitments, which are matching with bitmap and are enough",
		cns.ComputeSize(SrCommitment), len(cns.consensusGroup))
	log.Info("Step 4: SubroundId <COMMITMENT> has been finished")
}

// PrintSignatureCM method prints the <SIGNATURE> consensus messages
func (cns *Consensus) PrintSignatureCM() {
	log.Info("Step 5: Received %d from %d signatures, which are matching with bitmap and are enough",
		cns.ComputeSize(SrSignature), len(cns.consensusGroup))
	log.Info("Step 5: SubroundId <SIGNATURE> has been finished")
}
