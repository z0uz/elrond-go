package factory

import (
	"fmt"

	"github.com/ElrondNetwork/elrond-go/core"
	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/core/random"
	"github.com/ElrondNetwork/elrond-go/core/throttler"
	"github.com/ElrondNetwork/elrond-go/data/state"
	"github.com/ElrondNetwork/elrond-go/dataRetriever"
	"github.com/ElrondNetwork/elrond-go/dataRetriever/resolvers"
	"github.com/ElrondNetwork/elrond-go/dataRetriever/resolvers/topicResolverSender"
	"github.com/ElrondNetwork/elrond-go/marshal"
	"github.com/ElrondNetwork/elrond-go/process/factory"
	"github.com/ElrondNetwork/elrond-go/sharding"
	"github.com/ElrondNetwork/elrond-go/update"
	"github.com/ElrondNetwork/elrond-go/update/genesis"
)

type resolversContainerFactory struct {
	shardCoordinator       sharding.Coordinator
	messenger              dataRetriever.TopicMessageHandler
	marshalizer            marshal.Marshalizer
	intRandomizer          dataRetriever.IntRandomizer
	dataTrieContainer      state.TriesHolder
	container              dataRetriever.ResolversContainer
	inputAntifloodHandler  dataRetriever.P2PAntifloodHandler
	outputAntifloodHandler dataRetriever.P2PAntifloodHandler
	throttler              dataRetriever.ResolverThrottler
}

// ArgsNewResolversContainerFactory defines the arguments for the resolversContainerFactory constructor
type ArgsNewResolversContainerFactory struct {
	ShardCoordinator           sharding.Coordinator
	Messenger                  dataRetriever.TopicMessageHandler
	Marshalizer                marshal.Marshalizer
	DataTrieContainer          state.TriesHolder
	ExistingResolvers          dataRetriever.ResolversContainer
	InputAntifloodHandler      dataRetriever.P2PAntifloodHandler
	OutputAntifloodHandler     dataRetriever.P2PAntifloodHandler
	NumConcurrentResolvingJobs int32
}

// NewResolversContainerFactory creates a new container filled with topic resolvers
func NewResolversContainerFactory(args ArgsNewResolversContainerFactory) (*resolversContainerFactory, error) {
	if check.IfNil(args.ShardCoordinator) {
		return nil, update.ErrNilShardCoordinator
	}
	if check.IfNil(args.Messenger) {
		return nil, update.ErrNilMessenger
	}
	if check.IfNil(args.Marshalizer) {
		return nil, update.ErrNilMarshalizer
	}
	if check.IfNil(args.DataTrieContainer) {
		return nil, update.ErrNilTrieDataGetter
	}
	if check.IfNil(args.ExistingResolvers) {
		return nil, update.ErrNilResolverContainer
	}
	if check.IfNil(args.InputAntifloodHandler) {
		return nil, fmt.Errorf("%w on the input side", update.ErrNilAntifloodHandler)
	}
	if check.IfNil(args.OutputAntifloodHandler) {
		return nil, fmt.Errorf("%w on the output side", update.ErrNilAntifloodHandler)
	}

	thr, err := throttler.NewNumGoRoutinesThrottler(args.NumConcurrentResolvingJobs)
	if err != nil {
		return nil, err
	}

	return &resolversContainerFactory{
		shardCoordinator:       args.ShardCoordinator,
		messenger:              args.Messenger,
		marshalizer:            args.Marshalizer,
		intRandomizer:          &random.ConcurrentSafeIntRandomizer{},
		dataTrieContainer:      args.DataTrieContainer,
		container:              args.ExistingResolvers,
		throttler:              thr,
		inputAntifloodHandler:  args.InputAntifloodHandler,
		outputAntifloodHandler: args.OutputAntifloodHandler,
	}, nil
}

// Create returns a resolver container that will hold all resolvers in the system
func (rcf *resolversContainerFactory) Create() (dataRetriever.ResolversContainer, error) {
	err := rcf.generateTrieNodesResolvers()
	if err != nil {
		return nil, err
	}

	return rcf.container, nil
}

func (rcf *resolversContainerFactory) generateTrieNodesResolvers() error {
	shardC := rcf.shardCoordinator

	keys := make([]string, 0)
	resolversSlice := make([]dataRetriever.Resolver, 0)

	for i := uint32(0); i < shardC.NumberOfShards(); i++ {
		identifierTrieNodes := factory.AccountTrieNodesTopic + core.CommunicationIdentifierBetweenShards(i, core.MetachainShardId)
		if rcf.checkIfResolverExists(identifierTrieNodes) {
			continue
		}

		trieId := genesis.CreateTrieIdentifier(i, state.UserAccount)
		resolver, err := rcf.createTrieNodesResolver(identifierTrieNodes, trieId)
		if err != nil {
			return err
		}

		resolversSlice = append(resolversSlice, resolver)
		keys = append(keys, identifierTrieNodes)
	}

	identifierTrieNodes := factory.AccountTrieNodesTopic + core.CommunicationIdentifierBetweenShards(core.MetachainShardId, core.MetachainShardId)
	if !rcf.checkIfResolverExists(identifierTrieNodes) {
		trieId := genesis.CreateTrieIdentifier(core.MetachainShardId, state.UserAccount)
		resolver, err := rcf.createTrieNodesResolver(identifierTrieNodes, trieId)
		if err != nil {
			return err
		}

		resolversSlice = append(resolversSlice, resolver)
		keys = append(keys, identifierTrieNodes)
	}

	identifierTrieNodes = factory.ValidatorTrieNodesTopic + core.CommunicationIdentifierBetweenShards(core.MetachainShardId, core.MetachainShardId)
	if !rcf.checkIfResolverExists(identifierTrieNodes) {
		trieID := genesis.CreateTrieIdentifier(core.MetachainShardId, state.ValidatorAccount)
		resolver, err := rcf.createTrieNodesResolver(identifierTrieNodes, trieID)
		if err != nil {
			return err
		}

		resolversSlice = append(resolversSlice, resolver)
		keys = append(keys, identifierTrieNodes)
	}

	return rcf.container.AddMultiple(keys, resolversSlice)
}

func (rcf *resolversContainerFactory) checkIfResolverExists(topic string) bool {
	_, err := rcf.container.Get(topic)
	return err == nil
}

func (rcf *resolversContainerFactory) createTrieNodesResolver(baseTopic string, trieId string) (dataRetriever.Resolver, error) {
	excludePeersFromTopic := core.ConsensusTopic + rcf.shardCoordinator.CommunicationIdentifier(rcf.shardCoordinator.SelfId())

	peerListCreator, err := topicResolverSender.NewDiffPeerListCreator(rcf.messenger, baseTopic, excludePeersFromTopic)
	if err != nil {
		return nil, err
	}

	argResolver := topicResolverSender.ArgTopicResolverSender{
		Messenger:         rcf.messenger,
		TopicName:         baseTopic,
		PeerListCreator:   peerListCreator,
		Marshalizer:       rcf.marshalizer,
		Randomizer:        rcf.intRandomizer,
		TargetShardId:     rcf.shardCoordinator.SelfId(),
		OutputAntiflooder: rcf.outputAntifloodHandler,
	}

	resolverSender, err := topicResolverSender.NewTopicResolverSender(argResolver)
	if err != nil {
		return nil, err
	}

	argTrie := resolvers.ArgTrieNodeResolver{
		SenderResolver:   resolverSender,
		TrieDataGetter:   rcf.dataTrieContainer.Get([]byte(trieId)),
		Marshalizer:      rcf.marshalizer,
		AntifloodHandler: rcf.inputAntifloodHandler,
		Throttler:        rcf.throttler,
	}

	resolver, err := resolvers.NewTrieNodeResolver(argTrie)
	if err != nil {
		return nil, err
	}

	//add on the request topic
	return rcf.createTopicAndAssignHandler(
		resolverSender.RequestTopic(),
		resolver,
		false)
}

func (rcf *resolversContainerFactory) createTopicAndAssignHandler(
	topicName string,
	resolver dataRetriever.Resolver,
	createChannel bool,
) (dataRetriever.Resolver, error) {

	err := rcf.messenger.CreateTopic(topicName, createChannel)
	if err != nil {
		return nil, err
	}

	return resolver, rcf.messenger.RegisterMessageProcessor(topicName, resolver)
}

// IsInterfaceNil returns true if there is no value under the interface
func (rcf *resolversContainerFactory) IsInterfaceNil() bool {
	return rcf == nil
}