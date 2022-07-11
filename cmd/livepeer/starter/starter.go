package starter

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/golang/glog"
	"github.com/livepeer/go-livepeer/build"
	"github.com/livepeer/go-livepeer/common"
	"github.com/livepeer/go-livepeer/core"
	"github.com/livepeer/go-livepeer/discovery"
	"github.com/livepeer/go-livepeer/drivers"
	"github.com/livepeer/go-livepeer/eth"
	"github.com/livepeer/go-livepeer/eth/blockwatch"
	"github.com/livepeer/go-livepeer/eth/watchers"
	lpmon "github.com/livepeer/go-livepeer/monitor"
	"github.com/livepeer/go-livepeer/pm"
	"github.com/livepeer/go-livepeer/server"
	"github.com/livepeer/go-livepeer/verification"
	"github.com/livepeer/livepeer-data/pkg/event"
	"github.com/livepeer/lpms/ffmpeg"
)

var (
	// The timeout for ETH RPC calls
	ethRPCTimeout = 20 * time.Second
	// The maximum blocks for the block watcher to retain
	blockWatcherRetentionLimit = 20

	// Estimate of the gas required to redeem a PM ticket on L1 Ethereum
	redeemGasL1 = 350000
	// Estimate of the gas required to redeem a PM ticket on L2 Arbitrum
	redeemGasL2 = 1200000
	// The multiplier on the transaction cost to use for PM ticket faceValue
	txCostMultiplier = 100

	// The interval at which to clean up cached max float values for PM senders and balances per stream
	cleanupInterval = 1 * time.Minute
	// The time to live for cached max float values for PM senders (else they will be cleaned up) in seconds
	smTTL = 60 // 1 minute
)

const RtmpPort = "1935"
const RpcPort = "8935"
const CliPort = "7935"

type LivepeerConfig struct {
	Network                      *string
	RtmpAddr                     *string
	CliAddr                      *string
	HttpAddr                     *string
	ServiceAddr                  *string
	OrchAddr                     *string
	VerifierURL                  *string
	EthController                *string
	VerifierPath                 *string
	LocalVerify                  *bool
	HttpIngest                   *bool
	Orchestrator                 *bool
	Transcoder                   *bool
	Broadcaster                  *bool
	OrchSecret                   *string
	TranscodingOptions           *string
	MaxAttempts                  *int
	SelectRandFreq               *float64
	MaxSessions                  *int
	CurrentManifest              *bool
	Nvidia                       *string
	Netint                       *string
	TestTranscoder               *bool
	SceneClassificationModelPath *string
	DetectContent                *bool
	EthAcctAddr                  *string
	EthPassword                  *string
	EthKeystorePath              *string
	EthOrchAddr                  *string
	EthUrl                       *string
	TxTimeout                    *time.Duration
	MaxTxReplacements            *int
	GasLimit                     *int
	MinGasPrice                  *int64
	MaxGasPrice                  *int
	InitializeRound              *bool
	TicketEV                     *string
	MaxFaceValue                 *string
	MaxTicketEV                  *string
	DepositMultiplier            *int
	PricePerUnit                 *int
	MaxPricePerUnit              *int
	PixelsPerUnit                *int
	AutoAdjustPrice              *bool
	BlockPollingInterval         *int
	Redeemer                     *bool
	RedeemerAddr                 *string
	Reward                       *bool
	Monitor                      *bool
	MetricsPerStream             *bool
	MetricsExposeClientIP        *bool
	MetadataQueueUri             *string
	MetadataAmqpExchange         *string
	MetadataPublishTimeout       *time.Duration
	Datadir                      *string
	Objectstore                  *string
	Recordstore                  *string
	FVfailGsBucket               *string
	FVfailGsKey                  *string
	AuthWebhookURL               *string
	OrchWebhookURL               *string
	DetectionWebhookURL          *string
}

// DefaultLivepeerConfig creates LivepeerConfig exactly the same as when no flags are passed to the livepeer process.
func DefaultLivepeerConfig() LivepeerConfig {
	// Network & Addresses:
	defaultNetwork := "offchain"
	defaultRtmpAddr := "127.0.0.1:" + RtmpPort
	defaultCliAddr := "127.0.0.1:" + CliPort
	defaultHttpAddr := ""
	defaultServiceAddr := ""
	defaultOrchAddr := ""
	defaultVerifierURL := ""
	defaultVerifierPath := ""

	// Transcoding:
	defaultOrchestrator := false
	defaultTranscoder := false
	defaultBroadcaster := false
	defaultOrchSecret := ""
	defaultTranscodingOptions := "P240p30fps16x9,P360p30fps16x9"
	defaultMaxAttempts := 3
	defaultSelectRandFreq := 0.3
	defaultMaxSessions := 10
	defaultCurrentManifest := false
	defaultNvidia := ""
	defaultNetint := ""
	defaultTestTranscoder := true
	defaultDetectContent := false
	defaultSceneClassificationModelPath := "tasmodel.pb"

	// Onchain:
	defaultEthAcctAddr := ""
	defaultEthPassword := ""
	defaultEthKeystorePath := ""
	defaultEthOrchAddr := ""
	defaultEthUrl := ""
	defaultTxTimeout := 5 * time.Minute
	defaultMaxTxReplacements := 1
	defaultGasLimit := 0
	defaultMaxGasPrice := 0
	defaultEthController := ""
	defaultInitializeRound := false
	defaultTicketEV := "1000000000000"
	defaultMaxFaceValue := "0"
	defaultMaxTicketEV := "3000000000000"
	defaultDepositMultiplier := 1
	defaultMaxPricePerUnit := 0
	defaultPixelsPerUnit := 1
	defaultAutoAdjustPrice := true
	defaultBlockPollingInterval := 5
	defaultRedeemer := false
	defaultRedeemerAddr := ""
	defaultMonitor := false
	defaultMetricsPerStream := false
	defaultMetricsExposeClientIP := false
	defaultMetadataQueueUri := ""
	defaultMetadataAmqpExchange := "lp_golivepeer_metadata"
	defaultMetadataPublishTimeout := 1 * time.Second

	// Storage:
	defaultDatadir := ""
	defaultObjectstore := ""
	defaultRecordstore := ""

	// Fast Verification GS bucket:
	defaultFVfailGsBucket := ""
	defaultFVfailGsKey := ""

	// API
	defaultAuthWebhookURL := ""
	defaultOrchWebhookURL := ""
	defaultDetectionWebhookURL := ""

	return LivepeerConfig{
		// Network & Addresses:
		Network:      &defaultNetwork,
		RtmpAddr:     &defaultRtmpAddr,
		CliAddr:      &defaultCliAddr,
		HttpAddr:     &defaultHttpAddr,
		ServiceAddr:  &defaultServiceAddr,
		OrchAddr:     &defaultOrchAddr,
		VerifierURL:  &defaultVerifierURL,
		VerifierPath: &defaultVerifierPath,

		// Transcoding:
		Orchestrator:                 &defaultOrchestrator,
		Transcoder:                   &defaultTranscoder,
		Broadcaster:                  &defaultBroadcaster,
		OrchSecret:                   &defaultOrchSecret,
		TranscodingOptions:           &defaultTranscodingOptions,
		MaxAttempts:                  &defaultMaxAttempts,
		SelectRandFreq:               &defaultSelectRandFreq,
		MaxSessions:                  &defaultMaxSessions,
		CurrentManifest:              &defaultCurrentManifest,
		Nvidia:                       &defaultNvidia,
		Netint:                       &defaultNetint,
		TestTranscoder:               &defaultTestTranscoder,
		SceneClassificationModelPath: &defaultSceneClassificationModelPath,
		DetectContent:                &defaultDetectContent,

		// Onchain:
		EthAcctAddr:            &defaultEthAcctAddr,
		EthPassword:            &defaultEthPassword,
		EthKeystorePath:        &defaultEthKeystorePath,
		EthOrchAddr:            &defaultEthOrchAddr,
		EthUrl:                 &defaultEthUrl,
		TxTimeout:              &defaultTxTimeout,
		MaxTxReplacements:      &defaultMaxTxReplacements,
		GasLimit:               &defaultGasLimit,
		MaxGasPrice:            &defaultMaxGasPrice,
		EthController:          &defaultEthController,
		InitializeRound:        &defaultInitializeRound,
		TicketEV:               &defaultTicketEV,
		MaxFaceValue:           &defaultMaxFaceValue,
		MaxTicketEV:            &defaultMaxTicketEV,
		DepositMultiplier:      &defaultDepositMultiplier,
		MaxPricePerUnit:        &defaultMaxPricePerUnit,
		PixelsPerUnit:          &defaultPixelsPerUnit,
		AutoAdjustPrice:        &defaultAutoAdjustPrice,
		BlockPollingInterval:   &defaultBlockPollingInterval,
		Redeemer:               &defaultRedeemer,
		RedeemerAddr:           &defaultRedeemerAddr,
		Monitor:                &defaultMonitor,
		MetricsPerStream:       &defaultMetricsPerStream,
		MetricsExposeClientIP:  &defaultMetricsExposeClientIP,
		MetadataQueueUri:       &defaultMetadataQueueUri,
		MetadataAmqpExchange:   &defaultMetadataAmqpExchange,
		MetadataPublishTimeout: &defaultMetadataPublishTimeout,

		// Storage:
		Datadir:     &defaultDatadir,
		Objectstore: &defaultObjectstore,
		Recordstore: &defaultRecordstore,

		// Fast Verification GS bucket:
		FVfailGsBucket: &defaultFVfailGsBucket,
		FVfailGsKey:    &defaultFVfailGsKey,

		// API
		AuthWebhookURL:      &defaultAuthWebhookURL,
		OrchWebhookURL:      &defaultOrchWebhookURL,
		DetectionWebhookURL: &defaultDetectionWebhookURL,
	}
}

func StartLivepeer(ctx context.Context, cfg LivepeerConfig) {
	if *cfg.MaxSessions <= 0 {
		glog.Fatal("-maxSessions must be greater than zero")
		return
	}

	if *cfg.Netint != "" && *cfg.Nvidia != "" {
		glog.Fatal("both -netint and -nvidia arguments specified, this is not supported")
		return
	}

	blockPollingTime := time.Duration(*cfg.BlockPollingInterval) * time.Second

	type NetworkConfig struct {
		ethController string
		minGasPrice   int64
		redeemGas     int
	}

	configOptions := map[string]*NetworkConfig{
		"rinkeby": {
			ethController: "0x9a9827455911a858E55f07911904fACC0D66027E",
			redeemGas:     redeemGasL1,
		},
		"arbitrum-one-rinkeby": {
			ethController: "0x9ceC649179e2C7Ab91688271bcD09fb707b3E574",
			redeemGas:     redeemGasL2,
		},
		"mainnet": {
			ethController: "0xf96d54e490317c557a967abfa5d6e33006be69b3",
			minGasPrice:   int64(params.GWei),
			redeemGas:     redeemGasL1,
		},
		"arbitrum-one-mainnet": {
			ethController: "0xD8E8328501E9645d16Cf49539efC04f734606ee4",
			redeemGas:     redeemGasL2,
		},
	}

	// If multiple orchAddr specified, ensure other necessary flags present and clean up list
	orchURLs := parseOrchAddrs(*cfg.OrchAddr)

	// Setting config options based on specified network
	var redeemGas int
	minGasPrice := int64(0)
	if cfg.MinGasPrice != nil {
		minGasPrice = *cfg.MinGasPrice
	}
	if netw, ok := configOptions[*cfg.Network]; ok {
		if *cfg.EthController == "" {
			*cfg.EthController = netw.ethController
		}

		if cfg.MinGasPrice == nil {
			minGasPrice = netw.minGasPrice
		}

		redeemGas = netw.redeemGas

		glog.Infof("***Livepeer is running on the %v network: %v***", *cfg.Network, *cfg.EthController)
	} else {
		redeemGas = redeemGasL1
		glog.Infof("***Livepeer is running on the %v network***", *cfg.Network)
	}

	if *cfg.Datadir == "" {
		homedir := os.Getenv("HOME")
		if homedir == "" {
			usr, err := user.Current()
			if err != nil {
				glog.Fatalf("Cannot find current user: %v", err)
			}
			homedir = usr.HomeDir
		}
		*cfg.Datadir = filepath.Join(homedir, ".lpData", *cfg.Network)
	}

	//Make sure datadir is present
	if _, err := os.Stat(*cfg.Datadir); os.IsNotExist(err) {
		glog.Infof("Creating data dir: %v", *cfg.Datadir)
		if err = os.MkdirAll(*cfg.Datadir, 0755); err != nil {
			glog.Errorf("Error creating datadir: %v", err)
		}
	}

	//Set Gs bucket for fast verification fail case
	if *cfg.FVfailGsBucket != "" && *cfg.FVfailGsKey != "" {
		drivers.SetCreds(*cfg.FVfailGsBucket, *cfg.FVfailGsKey)
	}

	//Set up DB
	dbh, err := common.InitDB(*cfg.Datadir + "/lpdb.sqlite3")
	if err != nil {
		glog.Errorf("Error opening DB: %v", err)
		return
	}
	defer dbh.Close()

	n, err := core.NewLivepeerNode(nil, *cfg.Datadir, dbh)
	if err != nil {
		glog.Errorf("Error creating livepeer node: %v", err)
	}

	if *cfg.OrchSecret != "" {
		n.OrchSecret, _ = common.GetPass(*cfg.OrchSecret)
	}

	var transcoderCaps []core.Capability
	if *cfg.Transcoder {
		core.WorkDir = *cfg.Datadir
		accel := ffmpeg.Software
		var devicesStr string
		if *cfg.Nvidia != "" {
			accel = ffmpeg.Nvidia
			devicesStr = *cfg.Nvidia
		}
		if *cfg.Netint != "" {
			accel = ffmpeg.Netint
			devicesStr = *cfg.Netint
		}
		if accel != ffmpeg.Software {
			accelName := ffmpeg.AccelerationNameLookup[accel]
			tf, dtf, err := core.GetTranscoderFactoryByAccel(accel)
			if err != nil {
				glog.Fatalf("Error unsupported acceleration: %v", err)
			}
			// Get a list of device ids
			devices, err := common.ParseAccelDevices(devicesStr, accel)
			glog.Infof("%v devices: %v", accelName, devices)
			if err != nil {
				glog.Fatalf("Error while parsing '-%v %v' flag: %v", strings.ToLower(accelName), devices, err)
			}
			glog.Infof("Transcoding on these %v devices: %v", accelName, devices)
			// Test transcoding with specified device
			if *cfg.TestTranscoder {
				transcoderCaps, err = core.TestTranscoderCapabilities(devices, tf)
				if err != nil {
					glog.Fatal(err)
					return
				}
			} else {
				// no capability test was run, assume default capabilities
				transcoderCaps = append(transcoderCaps, core.DefaultCapabilities()...)
			}
			// initialize Tensorflow runtime on each device to reduce delay when creating new transcoding session
			if accel == ffmpeg.Nvidia && *cfg.DetectContent {
				if _, err := os.Stat(*cfg.SceneClassificationModelPath); err == nil {
					detectorProfile := ffmpeg.DSceneAdultSoccer
					detectorProfile.ModelPath = *cfg.SceneClassificationModelPath
					core.DetectorProfile = &detectorProfile
					for _, d := range devices {
						tc, err := core.NewNvidiaTranscoderWithDetector(&detectorProfile, d)
						if err != nil {
							glog.Fatalf("Could not initialize content detector")
						}
						defer tc.Stop()
					}
					// add SceneClassification capability
					transcoderCaps = append(transcoderCaps, core.Capability_SceneClassification)
				} else {
					glog.Fatalf("Content detection is enabled, but the model file '%s' does not exist", *cfg.SceneClassificationModelPath)
				}
			}
			// Initialize LB transcoder
			n.Transcoder = core.NewLoadBalancingTranscoder(devices, tf, dtf)
		} else {
			// for local software mode, enable all capabilities
			transcoderCaps = append(core.DefaultCapabilities(), core.OptionalCapabilities()...)
			n.Transcoder = core.NewLocalTranscoder(*cfg.Datadir)
		}
	}

	if *cfg.Redeemer {
		n.NodeType = core.RedeemerNode
	} else if *cfg.Orchestrator {
		n.NodeType = core.OrchestratorNode
		if !*cfg.Transcoder {
			n.TranscoderManager = core.NewRemoteTranscoderManager()
			n.Transcoder = n.TranscoderManager
		}
	} else if *cfg.Transcoder {
		n.NodeType = core.TranscoderNode
	} else if *cfg.Broadcaster {
		n.NodeType = core.BroadcasterNode
	} else if (cfg.Reward == nil || !*cfg.Reward) && !*cfg.InitializeRound {
		glog.Fatalf("No services enabled; must be at least one of -broadcaster, -transcoder, -orchestrator, -redeemer, -reward or -initializeRound")
	}

	lpmon.NodeID = *cfg.EthAcctAddr
	if lpmon.NodeID != "" {
		lpmon.NodeID += "-"
	}
	hn, _ := os.Hostname()
	lpmon.NodeID += hn

	if *cfg.Monitor {
		if *cfg.MetricsExposeClientIP {
			*cfg.MetricsPerStream = true
		}
		lpmon.Enabled = true
		lpmon.PerStreamMetrics = *cfg.MetricsPerStream
		lpmon.ExposeClientIP = *cfg.MetricsExposeClientIP
		nodeType := lpmon.Default
		switch n.NodeType {
		case core.BroadcasterNode:
			nodeType = lpmon.Broadcaster
		case core.OrchestratorNode:
			nodeType = lpmon.Orchestrator
		case core.TranscoderNode:
			nodeType = lpmon.Transcoder
		case core.RedeemerNode:
			nodeType = lpmon.Redeemer
		}
		lpmon.InitCensus(nodeType, core.LivepeerVersion)
	}

	watcherErr := make(chan error)
	serviceErr := make(chan error)
	var timeWatcher *watchers.TimeWatcher
	if *cfg.Network == "offchain" {
		glog.Infof("***Livepeer is in off-chain mode***")

		if err := checkOrStoreChainID(dbh, big.NewInt(0)); err != nil {
			glog.Error(err)
			return
		}

	} else {
		var keystoreDir string
		if _, err := os.Stat(*cfg.EthKeystorePath); !os.IsNotExist(err) {
			keystoreDir, _ = filepath.Split(*cfg.EthKeystorePath)
		} else {
			keystoreDir = filepath.Join(*cfg.Datadir, "keystore")
		}

		if keystoreDir == "" {
			glog.Errorf("Cannot find keystore directory")
			return
		}

		//Get the Eth client connection information
		if *cfg.EthUrl == "" {
			glog.Fatal("Need to specify an Ethereum node JSON-RPC URL using -ethUrl")
		}

		//Set up eth client
		backend, err := ethclient.Dial(*cfg.EthUrl)
		if err != nil {
			glog.Errorf("Failed to connect to Ethereum client: %v", err)
			return
		}

		chainID, err := backend.ChainID(ctx)
		if err != nil {
			glog.Errorf("failed to get chain ID from remote ethereum node: %v", err)
			return
		}

		if !build.ChainSupported(chainID.Int64()) {
			glog.Errorf("node does not support chainID = %v right now", chainID)
			return
		}

		if err := checkOrStoreChainID(dbh, chainID); err != nil {
			glog.Error(err)
			return
		}

		var bigMaxGasPrice *big.Int
		if *cfg.MaxGasPrice > 0 {
			bigMaxGasPrice = big.NewInt(int64(*cfg.MaxGasPrice))
		}

		gpm := eth.NewGasPriceMonitor(backend, blockPollingTime, big.NewInt(minGasPrice), bigMaxGasPrice)
		// Start gas price monitor
		_, err = gpm.Start(ctx)
		if err != nil {
			glog.Errorf("Error starting gas price monitor: %v", err)
			return
		}
		defer gpm.Stop()

		am, err := eth.NewAccountManager(ethcommon.HexToAddress(*cfg.EthAcctAddr), keystoreDir, chainID, *cfg.EthPassword)
		if err != nil {
			glog.Errorf("Error creating Ethereum account manager: %v", err)
			return
		}

		if err := am.Unlock(*cfg.EthPassword); err != nil {
			glog.Errorf("Error unlocking Ethereum account: %v", err)
			return
		}

		tm := eth.NewTransactionManager(backend, gpm, am, *cfg.TxTimeout, *cfg.MaxTxReplacements)
		go tm.Start()
		defer tm.Stop()

		ethCfg := eth.LivepeerEthClientConfig{
			AccountManager:     am,
			ControllerAddr:     ethcommon.HexToAddress(*cfg.EthController),
			EthClient:          backend,
			GasPriceMonitor:    gpm,
			TransactionManager: tm,
			Signer:             types.LatestSignerForChainID(chainID),
			CheckTxTimeout:     time.Duration(int64(*cfg.TxTimeout) * int64(*cfg.MaxTxReplacements+1)),
		}

		client, err := eth.NewClient(ethCfg)
		if err != nil {
			glog.Errorf("Failed to create Livepeer Ethereum client: %v", err)
			return
		}

		if err := client.SetGasInfo(uint64(*cfg.GasLimit)); err != nil {
			glog.Errorf("Failed to set gas info on Livepeer Ethereum Client: %v", err)
			return
		}
		if err := client.SetMaxGasPrice(bigMaxGasPrice); err != nil {
			glog.Errorf("Failed to set max gas price: %v", err)
			return
		}

		n.Eth = client

		addrMap := n.Eth.ContractAddresses()

		// Initialize block watcher that will emit logs used by event watchers
		blockWatcherClient, err := blockwatch.NewRPCClient(*cfg.EthUrl, ethRPCTimeout)
		if err != nil {
			glog.Errorf("Failed to setup blockwatch client: %v", err)
			return
		}
		topics := watchers.FilterTopics()

		blockWatcherCfg := blockwatch.Config{
			Store:               n.Database,
			PollingInterval:     blockPollingTime,
			StartBlockDepth:     rpc.LatestBlockNumber,
			BlockRetentionLimit: blockWatcherRetentionLimit,
			WithLogs:            true,
			Topics:              topics,
			Client:              blockWatcherClient,
		}
		// Wait until all event watchers have been initialized before starting the block watcher
		blockWatcher := blockwatch.New(blockWatcherCfg)

		timeWatcher, err = watchers.NewTimeWatcher(addrMap["RoundsManager"], blockWatcher, n.Eth)
		if err != nil {
			glog.Errorf("Failed to setup roundswatcher: %v", err)
			return
		}

		timeWatcherErr := make(chan error, 1)
		go func() {
			if err := timeWatcher.Watch(); err != nil {
				timeWatcherErr <- fmt.Errorf("roundswatcher failed to start watching for events: %v", err)
			}
		}()
		defer timeWatcher.Stop()

		// Initialize unbonding watcher to update the DB with latest state of the node's unbonding locks
		unbondingWatcher, err := watchers.NewUnbondingWatcher(n.Eth.Account().Address, addrMap["BondingManager"], blockWatcher, n.Database)
		if err != nil {
			glog.Errorf("Failed to setup unbonding watcher: %v", err)
			return
		}
		// Start unbonding watcher (logs will not be received until the block watcher is started)
		go unbondingWatcher.Watch()
		defer unbondingWatcher.Stop()

		senderWatcher, err := watchers.NewSenderWatcher(addrMap["TicketBroker"], blockWatcher, n.Eth, timeWatcher)
		if err != nil {
			glog.Errorf("Failed to setup senderwatcher: %v", err)
			return
		}
		go senderWatcher.Watch()
		defer senderWatcher.Stop()

		orchWatcher, err := watchers.NewOrchestratorWatcher(addrMap["BondingManager"], blockWatcher, dbh, n.Eth, timeWatcher)
		if err != nil {
			glog.Errorf("Failed to setup orchestrator watcher: %v", err)
			return
		}
		go orchWatcher.Watch()
		defer orchWatcher.Stop()

		serviceRegistryWatcher, err := watchers.NewServiceRegistryWatcher(addrMap["ServiceRegistry"], blockWatcher, dbh, n.Eth)
		if err != nil {
			glog.Errorf("Failed to set up service registry watcher: %v", err)
			return
		}
		go serviceRegistryWatcher.Watch()
		defer serviceRegistryWatcher.Stop()

		n.Balances = core.NewAddressBalances(cleanupInterval)
		defer n.Balances.StopCleanup()

		// By default the ticket recipient is the node's address
		// If the address of an on-chain registered orchestrator is provided, then it should be specified as the ticket recipient
		recipientAddr := n.Eth.Account().Address
		if *cfg.EthOrchAddr != "" {
			recipientAddr = ethcommon.HexToAddress(*cfg.EthOrchAddr)
		}

		smCfg := &pm.LocalSenderMonitorConfig{
			Claimant:        recipientAddr,
			CleanupInterval: cleanupInterval,
			TTL:             smTTL,
			RedeemGas:       redeemGas,
			SuggestGasPrice: client.Backend().SuggestGasPrice,
			RPCTimeout:      ethRPCTimeout,
		}

		if *cfg.Orchestrator {
			// Set price per pixel base info
			if *cfg.PixelsPerUnit <= 0 {
				// Can't divide by 0
				panic(fmt.Errorf("-pixelsPerUnit must be > 0, provided %d", *cfg.PixelsPerUnit))
			}
			if cfg.PricePerUnit == nil {
				// Prevent orchestrators from unknowingly providing free transcoding
				panic(fmt.Errorf("-pricePerUnit must be set"))
			}
			if *cfg.PricePerUnit < 0 {
				panic(fmt.Errorf("-pricePerUnit must be >= 0, provided %d", *cfg.PricePerUnit))
			}
			n.SetBasePrice(big.NewRat(int64(*cfg.PricePerUnit), int64(*cfg.PixelsPerUnit)))
			glog.Infof("Price: %d wei for %d pixels\n ", *cfg.PricePerUnit, *cfg.PixelsPerUnit)

			n.AutoAdjustPrice = *cfg.AutoAdjustPrice

			ev, _ := new(big.Int).SetString(*cfg.TicketEV, 10)
			if ev == nil {
				glog.Errorf("-ticketEV must be a valid integer, but %v provided. Restart the node with a different valid value for -ticketEV", *cfg.TicketEV)
				return
			}

			if ev.Cmp(big.NewInt(0)) < 0 {
				glog.Errorf("-ticketEV must be greater than 0, but %v provided. Restart the node with a different valid value for -ticketEV", *cfg.TicketEV)
				return
			}

			if err := setupOrchestrator(n, recipientAddr); err != nil {
				glog.Errorf("Error setting up orchestrator: %v", err)
				return
			}

			sigVerifier := &pm.DefaultSigVerifier{}
			validator := pm.NewValidator(sigVerifier, timeWatcher)

			var sm pm.SenderMonitor
			if *cfg.RedeemerAddr != "" {
				*cfg.RedeemerAddr = defaultAddr(*cfg.RedeemerAddr, "127.0.0.1", RpcPort)
				rc, err := server.NewRedeemerClient(*cfg.RedeemerAddr, senderWatcher, timeWatcher)
				if err != nil {
					glog.Error("Unable to start redeemer client: ", err)
					return
				}
				sm = rc
			} else {
				sm = pm.NewSenderMonitor(smCfg, n.Eth, senderWatcher, timeWatcher, n.Database)
			}

			// Start sender monitor
			sm.Start()
			defer sm.Stop()

			tcfg := pm.TicketParamsConfig{
				EV:               ev,
				RedeemGas:        redeemGas,
				TxCostMultiplier: txCostMultiplier,
			}
			n.Recipient, err = pm.NewRecipient(
				recipientAddr,
				n.Eth,
				validator,
				gpm,
				sm,
				timeWatcher,
				tcfg,
			)
			if err != nil {
				glog.Errorf("Error setting up PM recipient: %v", err)
				return
			}
			mfv, _ := new(big.Int).SetString(*cfg.MaxFaceValue, 10)
			if mfv == nil {
				panic(fmt.Errorf("-maxFaceValue must be a valid integer, but %v provided. Restart the node with a different valid value for -maxFaceValue", *cfg.MaxFaceValue))
				return
			} else {
				n.SetMaxFaceValue(mfv)
			}

		}

		if n.NodeType == core.BroadcasterNode {
			ev, _ := new(big.Rat).SetString(*cfg.MaxTicketEV)
			if ev == nil {
				panic(fmt.Errorf("-maxTicketEV must be a valid rational number, but %v provided. Restart the node with a valid value for -maxTicketEV", *cfg.MaxTicketEV))
			}

			if ev.Cmp(big.NewRat(0, 1)) < 0 {
				panic(fmt.Errorf("-maxTicketEV must not be negative, but %v provided. Restart the node with a valid value for -maxTicketEV", *cfg.MaxTicketEV))
			}

			if *cfg.DepositMultiplier <= 0 {
				panic(fmt.Errorf("-depositMultiplier must be greater than 0, but %v provided. Restart the node with a valid value for -depositMultiplier", *cfg.DepositMultiplier))
			}

			// Fetch and cache broadcaster on-chain info
			info, err := senderWatcher.GetSenderInfo(n.Eth.Account().Address)
			if err != nil {
				glog.Error("Failed to get broadcaster on-chain info: ", err)
				return
			}
			glog.Info("Broadcaster Deposit: ", eth.FormatUnits(info.Deposit, "ETH"))
			glog.Info("Broadcaster Reserve: ", eth.FormatUnits(info.Reserve.FundsRemaining, "ETH"))

			n.Sender = pm.NewSender(n.Eth, timeWatcher, senderWatcher, ev, *cfg.DepositMultiplier)

			if *cfg.PixelsPerUnit <= 0 {
				// Can't divide by 0
				panic(fmt.Errorf("The amount of pixels per unit must be greater than 0, provided %d instead\n", *cfg.PixelsPerUnit))
			}
			if *cfg.MaxPricePerUnit > 0 {
				server.BroadcastCfg.SetMaxPrice(big.NewRat(int64(*cfg.MaxPricePerUnit), int64(*cfg.PixelsPerUnit)))
			} else {
				glog.Infof("Maximum transcoding price per pixel is not greater than 0: %v, broadcaster is currently set to accept ANY price.\n", *cfg.MaxPricePerUnit)
				glog.Infoln("To update the broadcaster's maximum acceptable transcoding price per pixel, use the CLI or restart the broadcaster with the appropriate 'maxPricePerUnit' and 'pixelsPerUnit' values")
			}
		}

		if n.NodeType == core.RedeemerNode {
			if err := setupOrchestrator(n, recipientAddr); err != nil {
				glog.Errorf("Error setting up orchestrator: %v", err)
				return
			}

			r, err := server.NewRedeemer(
				recipientAddr,
				n.Eth,
				pm.NewSenderMonitor(smCfg, n.Eth, senderWatcher, timeWatcher, n.Database),
			)
			if err != nil {
				glog.Errorf("Unable to create redeemer: %v", err)
				return
			}

			*cfg.HttpAddr = defaultAddr(*cfg.HttpAddr, "127.0.0.1", RpcPort)
			url, err := url.ParseRequestURI("https://" + *cfg.HttpAddr)
			if err != nil {
				glog.Error("Could not parse redeemer URI: ", err)
				return
			}

			go func() {
				if err := r.Start(url, n.WorkDir); err != nil {
					serviceErr <- err
					return
				}
			}()
			defer r.Stop()
			glog.Infof("Redeemer started on %v", *cfg.HttpAddr)
		}

		var reward bool
		if cfg.Reward == nil {
			// If the node address is an on-chain registered address, start the reward service
			t, err := n.Eth.GetTranscoder(n.Eth.Account().Address)
			if err != nil {
				glog.Error(err)
				return
			}
			if t.Status == "Registered" {
				reward = true
			} else {
				reward = false
			}
		}

		if reward {
			// Start reward service
			// The node will only call reward if it is active in the current round
			rs := eth.NewRewardService(n.Eth, timeWatcher)
			go func() {
				if err := rs.Start(ctx); err != nil {
					serviceErr <- err
				}
				return
			}()
			defer rs.Stop()
		}

		if *cfg.InitializeRound {
			// Start round initializer
			// The node will only initialize rounds if it in the upcoming active set for the round
			initializer := eth.NewRoundInitializer(n.Eth, timeWatcher)
			go func() {
				if err := initializer.Start(); err != nil {
					serviceErr <- err
				}
				return
			}()
			defer initializer.Stop()
		}

		blockWatchCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Backfill events that the node has missed since its last seen block. This method will block
		// and the node will not continue setup until it finishes
		glog.Infof("Backfilling block events (this can take a while)...\n")
		if err := blockWatcher.BackfillEvents(blockWatchCtx); err != nil {
			glog.Errorf("Failed to backfill events: %v", err)
			return
		}
		glog.Info("Done backfilling block events")

		blockWatcherErr := make(chan error, 1)
		go func() {
			if err := blockWatcher.Watch(blockWatchCtx); err != nil {
				blockWatcherErr <- fmt.Errorf("block watcher error: %v", err)
			}
		}()

		go func() {
			var err error
			select {
			case err = <-timeWatcherErr:
			case err = <-blockWatcherErr:
			}

			watcherErr <- err
		}()
	}

	if *cfg.Objectstore != "" {
		prepared, err := drivers.PrepareOSURL(*cfg.Objectstore)
		if err != nil {
			glog.Error("Error creating object store driver: ", err)
			return
		}
		drivers.NodeStorage, err = drivers.ParseOSURL(prepared, false)
		if err != nil {
			glog.Error("Error creating object store driver: ", err)
			return
		}
	}

	if *cfg.Recordstore != "" {
		prepared, err := drivers.PrepareOSURL(*cfg.Recordstore)
		if err != nil {
			glog.Error("Error creating recordings object store driver: ", err)
			return
		}
		drivers.RecordStorage, err = drivers.ParseOSURL(prepared, true)
		if err != nil {
			glog.Error("Error creating recordings object store driver: ", err)
			return
		}
	}

	core.MaxSessions = *cfg.MaxSessions
	if lpmon.Enabled {
		lpmon.MaxSessions(core.MaxSessions)
	}

	if *cfg.AuthWebhookURL != "" {
		parsedUrl, err := validateURL(*cfg.AuthWebhookURL)
		if err != nil {
			glog.Fatal("Error setting auth webhook URL ", err)
		}
		glog.Info("Using auth webhook URL ", parsedUrl.Redacted())
		server.AuthWebhookURL = parsedUrl
	}

	if *cfg.DetectionWebhookURL != "" {
		parsedUrl, err := validateURL(*cfg.DetectionWebhookURL)
		if err != nil {
			glog.Fatal("Error setting detection webhook URL ", err)
		}
		glog.Info("Using detection webhook URL ", parsedUrl.Redacted())
		server.DetectionWebhookURL = parsedUrl
	}
	httpIngest := true

	if n.NodeType == core.BroadcasterNode {
		// default lpms listener for broadcaster; same as default rpc port
		// TODO provide an option to disable this?
		*cfg.RtmpAddr = defaultAddr(*cfg.RtmpAddr, "127.0.0.1", RtmpPort)
		*cfg.HttpAddr = defaultAddr(*cfg.HttpAddr, "127.0.0.1", RpcPort)

		bcast := core.NewBroadcaster(n)

		// When the node is on-chain mode always cache the on-chain orchestrators and poll for updates
		// Right now we rely on the DBOrchestratorPoolCache constructor to do this. Consider separating the logic
		// caching/polling from the logic for fetching orchestrators during discovery
		if *cfg.Network != "offchain" {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			dbOrchPoolCache, err := discovery.NewDBOrchestratorPoolCache(ctx, n, timeWatcher)
			if err != nil {
				glog.Errorf("Could not create orchestrator pool with DB cache: %v", err)
			}

			n.OrchestratorPool = dbOrchPoolCache
		}

		// Set up orchestrator discovery
		if *cfg.OrchWebhookURL != "" {
			whurl, err := validateURL(*cfg.OrchWebhookURL)
			if err != nil {
				glog.Fatal("Error setting orch webhook URL ", err)
			}
			glog.Info("Using orchestrator webhook URL ", whurl)
			n.OrchestratorPool = discovery.NewWebhookPool(bcast, whurl)
		} else if len(orchURLs) > 0 {
			n.OrchestratorPool = discovery.NewOrchestratorPool(bcast, orchURLs, common.Score_Trusted)
		}

		if n.OrchestratorPool == nil {
			// Not a fatal error; may continue operating in segment-only mode
			glog.Error("No orchestrator specified; transcoding will not happen")
		}

		isLocalHTTP, err := isLocalURL("https://" + *cfg.HttpAddr)
		if err != nil {
			glog.Errorf("Error checking for local -httpAddr: %v", err)
			return
		}
		if cfg.HttpIngest != nil {
			httpIngest = *cfg.HttpIngest
		}
		if cfg.HttpIngest == nil && !isLocalHTTP && server.AuthWebhookURL == nil {
			glog.Warning("HTTP ingest is disabled because -httpAddr is publicly accessible. To enable, configure -authWebhookUrl or use the -httpIngest flag")
			httpIngest = false
		}

		// Disable local verification when running in off-chain mode
		// To enable, set -localVerify or -verifierURL
		localVerify := true
		if cfg.LocalVerify != nil {
			localVerify = *cfg.LocalVerify
		}
		if cfg.LocalVerify == nil && *cfg.Network == "offchain" {
			localVerify = false
		}

		if *cfg.VerifierURL != "" {
			_, err := validateURL(*cfg.VerifierURL)
			if err != nil {
				glog.Fatal("Error setting verifier URL ", err)
			}
			glog.Info("Using the Epic Labs classifier for verification at ", *cfg.VerifierURL)
			server.Policy = &verification.Policy{Retries: 2, Verifier: &verification.EpicClassifier{Addr: *cfg.VerifierURL}}

			// Set the verifier path. Remove once [1] is implemented!
			// [1] https://github.com/livepeer/verification-classifier/issues/64
			if drivers.NodeStorage == nil && *cfg.VerifierPath == "" {
				glog.Fatal("Requires a path to the verifier shared volume when local storage is in use; use -verifierPath or -objectStore")
			}
			verification.VerifierPath = *cfg.VerifierPath
		} else if localVerify {
			glog.Info("Local verification enabled")
			server.Policy = &verification.Policy{Retries: 2}
		}

		// Set max transcode attempts. <=0 is OK; it just means "don't transcode"
		server.MaxAttempts = *cfg.MaxAttempts
		server.SelectRandFreq = *cfg.SelectRandFreq

	} else if n.NodeType == core.OrchestratorNode {
		suri, err := getServiceURI(n, *cfg.ServiceAddr)
		if err != nil {
			glog.Fatal("Error getting service URI: ", err)
		}
		n.SetServiceURI(suri)
		// if http addr is not provided, listen to all ifaces
		// take the port to listen to from the service URI
		*cfg.HttpAddr = defaultAddr(*cfg.HttpAddr, "", n.GetServiceURI().Port())
		if !*cfg.Transcoder && n.OrchSecret == "" {
			glog.Fatal("Running an orchestrator requires an -orchSecret for standalone mode or -transcoder for orchestrator+transcoder mode")
		}
	}
	n.Capabilities = core.NewCapabilities(transcoderCaps, core.MandatoryOCapabilities())
	*cfg.CliAddr = defaultAddr(*cfg.CliAddr, "127.0.0.1", CliPort)

	if drivers.NodeStorage == nil {
		// base URI will be empty for broadcasters; that's OK
		drivers.NodeStorage = drivers.NewMemoryDriver(n.GetServiceURI())
	}

	if *cfg.MetadataPublishTimeout > 0 {
		server.MetadataPublishTimeout = *cfg.MetadataPublishTimeout
	}
	if *cfg.MetadataQueueUri != "" {
		uri, err := url.ParseRequestURI(*cfg.MetadataQueueUri)
		if err != nil {
			glog.Fatalf("Error parsing -metadataQueueUri: err=%q", err)
		}
		switch uri.Scheme {
		case "amqp", "amqps":
			uriStr, exchange, keyNs := *cfg.MetadataQueueUri, *cfg.MetadataAmqpExchange, n.NodeType.String()
			server.MetadataQueue, err = event.NewAMQPExchangeProducer(context.Background(), uriStr, exchange, keyNs)
			if err != nil {
				glog.Fatalf("Error establishing AMQP connection: err=%q", err)
			}
		default:
			glog.Fatalf("Unsupported scheme in -metadataUri: %s", uri.Scheme)
		}
	}

	//Create Livepeer Node

	//Set up the media server
	s, err := server.NewLivepeerServer(*cfg.RtmpAddr, n, httpIngest, *cfg.TranscodingOptions)
	if err != nil {
		glog.Fatalf("Error creating Livepeer server: err=%q", err)
	}

	ec := make(chan error)
	tc := make(chan struct{})
	wc := make(chan struct{})
	msCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if *cfg.CurrentManifest {
		glog.Info("Current ManifestID will be available over ", *cfg.HttpAddr)
		s.ExposeCurrentManifest = *cfg.CurrentManifest
	}

	srv := &http.Server{Addr: *cfg.CliAddr}
	go func() {
		s.StartCliWebserver(srv)
		close(wc)
	}()
	if n.NodeType != core.RedeemerNode {
		go func() {
			ec <- s.StartMediaServer(msCtx, *cfg.HttpAddr)
		}()
	}

	go func() {
		if core.OrchestratorNode != n.NodeType {
			return
		}

		orch := core.NewOrchestrator(s.LivepeerNode, timeWatcher)

		go func() {
			err = server.StartTranscodeServer(orch, *cfg.HttpAddr, s.HTTPMux, n.WorkDir, n.TranscoderManager != nil)
			if err != nil {
				glog.Fatalf("Error starting Transcoder node: err=%q", err)
			}
			tc <- struct{}{}
		}()

		// check whether or not the orchestrator is available
		time.Sleep(2 * time.Second)
		orchAvail := server.CheckOrchestratorAvailability(orch)
		if !orchAvail {
			// shut down orchestrator
			glog.Infof("Orchestrator not available at %v; shutting down", orch.ServiceURI())
			tc <- struct{}{}
		}

	}()

	if n.NodeType == core.TranscoderNode {
		if n.OrchSecret == "" {
			glog.Fatal("Missing -orchSecret")
		}
		if len(orchURLs) <= 0 {
			glog.Fatal("Missing -orchAddr")
		}

		go server.RunTranscoder(n, orchURLs[0].Host, *cfg.MaxSessions, transcoderCaps)
	}

	switch n.NodeType {
	case core.OrchestratorNode:
		glog.Infof("***Livepeer Running in Orchestrator Mode***")
	case core.BroadcasterNode:
		glog.Infof("***Livepeer Running in Broadcaster Mode***")
		glog.Infof("Video Ingest Endpoint - rtmp://%v", *cfg.RtmpAddr)
	case core.TranscoderNode:
		glog.Infof("**Liveepeer Running in Transcoder Mode***")
	case core.RedeemerNode:
		glog.Infof("**Livepeer Running in Redeemer Mode**")
	}

	glog.Infof("Livepeer Node version: %v", core.LivepeerVersion)

	select {
	case err := <-watcherErr:
		glog.Error(err)
		return
	case err := <-ec:
		glog.Infof("Error from media server: %v", err)
		return
	case err := <-serviceErr:
		if err != nil {
			glog.Fatalf("Error starting service: %v", err)
		}
	case <-tc:
		glog.Infof("Orchestrator server shut down")
	case <-wc:
		glog.Infof("CLI webserver shut down")
		return
	case <-msCtx.Done():
		glog.Infof("MediaServer Done()")
		return
	case <-ctx.Done():
		srv.Shutdown(ctx)
		return
	}
}

func parseOrchAddrs(addrs string) []*url.URL {
	var res []*url.URL
	if len(addrs) > 0 {
		for _, addr := range strings.Split(addrs, ",") {
			addr = strings.TrimSpace(addr)
			addr = defaultAddr(addr, "127.0.0.1", RpcPort)
			if !strings.HasPrefix(addr, "http") {
				addr = "https://" + addr
			}
			uri, err := url.ParseRequestURI(addr)
			if err != nil {
				glog.Error("Could not parse orchestrator URI: ", err)
				continue
			}
			res = append(res, uri)
		}
	}
	return res
}

func validateURL(u string) (*url.URL, error) {
	if u == "" {
		return nil, nil
	}
	p, err := url.ParseRequestURI(u)
	if err != nil {
		return nil, err
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return nil, errors.New("URL should be HTTP or HTTPS")
	}
	return p, nil
}

func isLocalURL(u string) (bool, error) {
	uri, err := url.ParseRequestURI(u)
	if err != nil {
		return false, err
	}

	hostname := uri.Hostname()
	if net.ParseIP(hostname).IsLoopback() || hostname == "localhost" {
		return true, nil
	}

	return false, nil
}

// ServiceURI checking steps:
// If passed in via -serviceAddr: return that
// Else: get inferred address.
// If offchain: return inferred address
// Else: get on-chain sURI
// If on-chain sURI mismatches inferred address: print warning
// Return on-chain sURI
func getServiceURI(n *core.LivepeerNode, serviceAddr string) (*url.URL, error) {
	// Passed in via CLI
	if serviceAddr != "" {
		return url.ParseRequestURI("https://" + serviceAddr)
	}

	// Infer address
	// TODO probably should put this (along w wizard GETs) into common code
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		glog.Errorf("Could not look up public IP err=%q", err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf("Could not look up public IP err=%q", err)
		return nil, err
	}
	addr := "https://" + strings.TrimSpace(string(body)) + ":" + RpcPort
	inferredUri, err := url.ParseRequestURI(addr)
	if err != nil {
		glog.Errorf("Could not look up public IP err=%q", err)
		return nil, err
	}
	if n.Eth == nil {
		// we won't be looking up onchain sURI so use the inferred one
		return inferredUri, err
	}

	// On-chain lookup and matching with inferred public address
	addr, err = n.Eth.GetServiceURI(n.Eth.Account().Address)
	if err != nil {
		glog.Errorf("Could not get service URI; orchestrator may be unreachable err=%q", err)
		return nil, err
	}
	ethUri, err := url.ParseRequestURI(addr)
	if err != nil {
		glog.Errorf("Could not parse service URI; orchestrator may be unreachable err=%q", err)
		ethUri, _ = url.ParseRequestURI("http://127.0.0.1:" + RpcPort)
	}
	if ethUri.Hostname() != inferredUri.Hostname() || ethUri.Port() != inferredUri.Port() {
		glog.Errorf("Service address %v did not match discovered address %v; set the correct address in livepeer_cli or use -serviceAddr", ethUri, inferredUri)
	}
	return ethUri, nil
}

func setupOrchestrator(n *core.LivepeerNode, ethOrchAddr ethcommon.Address) error {
	// add orchestrator to DB
	orch, err := n.Eth.GetTranscoder(ethOrchAddr)
	if err != nil {
		return err
	}

	err = n.Database.UpdateOrch(&common.DBOrch{
		EthereumAddr:      ethOrchAddr.Hex(),
		ActivationRound:   common.ToInt64(orch.ActivationRound),
		DeactivationRound: common.ToInt64(orch.DeactivationRound),
	})
	if err != nil {
		return err
	}

	if !orch.Active {
		glog.Infof("Orchestrator %v is inactive", ethOrchAddr.Hex())
	} else {
		glog.Infof("Orchestrator %v is active", ethOrchAddr.Hex())
	}

	return nil
}

func defaultAddr(addr, defaultHost, defaultPort string) string {
	if addr == "" {
		return defaultHost + ":" + defaultPort
	}
	if addr[0] == ':' {
		return defaultHost + addr
	}
	// not IPv6 safe
	if !strings.Contains(addr, ":") {
		return addr + ":" + defaultPort
	}
	return addr
}

func checkOrStoreChainID(dbh *common.DB, chainID *big.Int) error {
	expectedChainID, err := dbh.ChainID()
	if err != nil {
		return err
	}

	if expectedChainID == nil {
		// No chainID stored yet
		// Store the provided chainID and skip the check
		if err := dbh.SetChainID(chainID); err != nil {
			return err
		}

		return nil
	}

	if expectedChainID.Cmp(chainID) != 0 {
		return fmt.Errorf("expecting chainID of %v, but got %v. Did you change networks without changing network name or datadir?", expectedChainID, chainID)
	}

	return nil
}