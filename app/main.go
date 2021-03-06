/******************************************************
# DESC    : the main process
# AUTHOR  : Alex Stocks
# VERSION : 1.0
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2018-03-22 20:45
# FILE    : main.go
******************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/goext/log"
	"github.com/AlexStocks/goext/net"
	"github.com/AlexStocks/goext/time"
)

const (
	APP_CONF_FILE           string = "APP_CONF_FILE"
	APP_LOG_CONF_FILE       string = "APP_LOG_CONF_FILE"
	APP_KAFKA_LOG_CONF_FILE string = "APP_KAFKA_LOG_CONF_FILE"
	APP_HTTP_LOG_CONF_FILE  string = "APP_HTTP_LOG_CONF_FILE"
)

const (
	FailfastTimeout  = 3 // in second
	KeepAliveTimeout = 1e9
)

var (
	pprofPath = "/debug/pprof/"

	usageStr = `
Usage: log-kafka [options]
Go runtime version %s
Server Options:
    -c, --config <file>              Configuration file path
    -l, --log <file>                 Log configuration file
    -k, --kafka_log <file>           Kafka Log configuration file
    -t, --http_log <file>            Http Log configuration file
Common Options:
    -h, --help                       Show this message
    -v, --version                    Show version
`
)

// usage will print out the flag options for the server.
func usage() {
	fmt.Printf(usageStr+"\n", runtime.Version())
	os.Exit(0)
}

func getHostInfo() {
	var (
		err error
	)

	Hostname, err := os.Hostname()
	if err != nil {
		panic(fmt.Sprintf("os.Hostname() = %s", err))
	}

	LocalIP, err = gxnet.GetLocalIP(LocalIP)
	if err != nil {
		panic("can not get local IP!")
	}

	ProcessID = fmt.Sprintf("%s@%s", LocalIP, Hostname)
}

func createPIDFile() error {
	if !Conf.Core.PID.Enabled {
		return nil
	}

	pidPath := Conf.Core.PID.Path
	_, err := os.Stat(pidPath)
	if os.IsNotExist(err) || Conf.Core.PID.Override {
		currentPid := os.Getpid()
		if err := os.MkdirAll(filepath.Dir(pidPath), os.ModePerm); err != nil {
			return fmt.Errorf("Can't create PID folder on %v", err)
		}

		file, err := os.Create(pidPath)
		if err != nil {
			return fmt.Errorf("Can't create PID file: %v", err)
		}
		defer file.Close()
		if _, err := file.WriteString(fmt.Sprintf("%s-%s", ProcessID, strconv.FormatInt(int64(currentPid), 10))); err != nil {
			return fmt.Errorf("Can'write PID information on %s: %v", pidPath, err)
		}
	} else {
		return fmt.Errorf("%s already exists", pidPath)
	}
	return nil
}

// initLog use for initial log module
func initLog(logConf string) {
	Log = gxlog.NewLoggerWithConfFile(logConf)
	Log.SetAsDefaultLogger()
}

// initKafkaLog use for kafka log module
func initKafkaLog(logConf string) {
	KafkaLog = gxlog.NewLoggerWithConfFile(logConf)
}

// initHttpLog use for http log module
func initHttpLog(logConf string) {
	HTTPLog = gxlog.NewLoggerWithConfFile(logConf)
}

func initWorker() {
	Worker = NewKafkaWorker()
	Worker.Start(int64(Conf.Core.WorkerNum), int64(Conf.Core.QueueNum))
}

func initSignal() {
	var (
		seq int
		// signal.Notify的ch信道是阻塞的(signal.Notify不会阻塞发送信号), 需要设置缓冲
		signals = make(chan os.Signal, 1)
		ticker  = time.NewTicker(KeepAliveTimeout)
	)
	// It is not possible to block SIGKILL or syscall.SIGSTOP
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case sig := <-signals:
			Log.Info("get signal %s", sig.String())
			switch sig {
			case syscall.SIGHUP:
			// reload()
			default:
				go gxtime.Future(Conf.Core.FailFastTimeout, func() {
					Log.Warn("app exit now by force...")
					os.Exit(1)
				})

				// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
				Server.Stop()
				KafkaLog.Close()
				HTTPLog.Close()
				Log.Warn("app exit now...")
				Log.Close()
				return
			}
		// case <-time.After(time.Duration(1e9)):
		case <-ticker.C:
			UpdateNow()
			seq++
			if seq%60 == 0 {
				Log.Info(Worker.Info())
			}
		}
	}
}

func main() {
	var (
		err          error
		showVersion  bool
		configFile   string
		logConf      string
		kafkaLogConf string
		httpLogConf  string
	)

	/////////////////////////////////////////////////
	// conf
	/////////////////////////////////////////////////

	SetVersion(Version)

	flag.BoolVar(&showVersion, "v", false, "Print version information.")
	flag.BoolVar(&showVersion, "version", false, "Print version information.")
	flag.StringVar(&configFile, "c", "", "Configuration file path.")
	flag.StringVar(&configFile, "config", "", "Configuration file path.")
	flag.StringVar(&logConf, "l", "", "Logger configuration file.")
	flag.StringVar(&logConf, "log", "", "Logger configuration file.")
	flag.StringVar(&kafkaLogConf, "k", "", "Kafka logger configuration file.")
	flag.StringVar(&kafkaLogConf, "kafka_log", "", "Kafka logger configuration file.")
	flag.StringVar(&httpLogConf, "t", "", "Http logger configuration file.")
	flag.StringVar(&httpLogConf, "http_log", "", "Http logger configuration file.")

	flag.Usage = usage
	flag.Parse()

	// Show version and exit
	if showVersion {
		PrintVersion()
		os.Exit(0)
	}

	if configFile == "" {
		configFile = os.Getenv(APP_CONF_FILE)
		if configFile == "" {
			panic("can not get configFile!")
		}
	}
	if path.Ext(configFile) != ".yml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .yml", configFile))
	}
	Conf, err = LoadConfYaml(configFile)
	if err != nil {
		log.Printf("Load yaml config file error: '%v'", err)
		return
	}
	fmt.Printf("config: %+v\n", Conf)

	if logConf == "" {
		logConf = os.Getenv(APP_LOG_CONF_FILE)
		if logConf == "" {
			panic("can not get logConf!")
		}
	}

	if kafkaLogConf == "" {
		kafkaLogConf = os.Getenv(APP_KAFKA_LOG_CONF_FILE)
		if kafkaLogConf == "" {
			usage()
		}
	}

	if httpLogConf == "" {
		httpLogConf = os.Getenv(APP_HTTP_LOG_CONF_FILE)
		if httpLogConf == "" {
			usage()
		}
	}

	/////////////////////////////////////////////////
	// worker
	/////////////////////////////////////////////////
	if Conf.Core.FailFastTimeout == 0 {
		Conf.Core.FailFastTimeout = FailfastTimeout
	}

	getHostInfo()

	initLog(logConf)
	initKafkaLog(kafkaLogConf)
	initHttpLog(httpLogConf)
	initAppStatus()

	for i := 0; i < 4096; i++ {
		Log.Debug("hello %d", i)
	}

	if err = createPIDFile(); err != nil {
		Log.Critic(err)
	}

	initHTTPServer()

	Server = NewUdpServer()
	initWorker()

	initSignal()
}
