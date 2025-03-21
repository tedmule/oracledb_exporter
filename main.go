package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	_ "github.com/sijms/go-ora/v2"

	kingpin "github.com/alecthomas/kingpin/v2"

	"go.uber.org/zap"

	// Required for debugging
	// _ "net/http/pprof"

	"github.com/iamseth/oracledb_exporter/collector"
)

var (
	// Version will be set at build time.
	Version    = "0.0.0.dev"
	metricPath = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics. (env: TELEMETRY_PATH)").Default(getEnv("TELEMETRY_PATH", "/metrics")).String()
	dsn        = kingpin.Flag("database.dsn",
		"Connection string to a data source. (env: DATA_SOURCE_NAME)",
	).Default(getEnv("DATA_SOURCE_NAME", "")).String()
	dsnFile = kingpin.Flag("database.dsnFile",
		"File to read a string to a data source from. (env: DATA_SOURCE_NAME_FILE)",
	).Default(getEnv("DATA_SOURCE_NAME_FILE", "")).String()
	defaultFileMetrics = kingpin.Flag(
		"default.metrics",
		"File with default metrics in a toml or yaml format. (env: DEFAULT_METRICS)",
	).Default(getEnv("DEFAULT_METRICS", "default-metrics.yaml")).String()
	customMetrics = kingpin.Flag(
		"custom.metrics",
		"File that may contain various custom metrics in a toml or yaml format. (env: CUSTOM_METRICS)",
	).Default(getEnv("CUSTOM_METRICS", "")).String()
	queryTimeout = kingpin.Flag(
		"query.timeout",
		"Query timeout (in seconds). (env: QUERY_TIMEOUT)",
	).Default(getEnv("QUERY_TIMEOUT", "5")).Int()
	maxIdleConns = kingpin.Flag(
		"database.maxIdleConns",
		"Number of maximum idle connections in the connection pool. (env: DATABASE_MAXIDLECONNS)",
	).Default(getEnv("DATABASE_MAXIDLECONNS", "0")).Int()
	maxOpenConns = kingpin.Flag(
		"database.maxOpenConns",
		"Number of maximum open connections in the connection pool. (env: DATABASE_MAXOPENCONNS)",
	).Default(getEnv("DATABASE_MAXOPENCONNS", "10")).Int()
	scrapeInterval = kingpin.Flag(
		"scrape.interval",
		"Interval between each scrape. Default is to scrape on collect requests",
	).Default("0s").Duration()
	toolkitFlags = webflag.AddFlags(kingpin.CommandLine, ":9161")
)

func main() {
	zapLogger, _ := zap.NewProduction()
	// zapLogger, _ := zap.NewDevelopment()
	defer zapLogger.Sync()

	// promsLogConfig := &promslog.Config{}
	// flag.AddFlags(kingpin.CommandLine, promsLogConfig)
	kingpin.HelpFlag.Short('\n')
	kingpin.Version(version.Print("oracledb_exporter"))
	kingpin.Parse()
	// logger := promslog.New(promsLogConfig)
	logger := zapLogger.Sugar()

	// DELETE <<<EOF
	if dsnFile != nil && *dsnFile != "" {
		dsnFileContent, err := os.ReadFile(*dsnFile)
		if err != nil {
			logger.Infow("msg", "Unable to read DATA_SOURCE_NAME_FILE", "file", dsnFile, "error", err)
			os.Exit(1)
		}
		*dsn = string(dsnFileContent)
	}
	//EOF

	config := &collector.Config{
		DSN:                *dsn,
		MaxOpenConns:       *maxOpenConns,
		MaxIdleConns:       *maxIdleConns,
		CustomMetrics:      *customMetrics,
		QueryTimeout:       *queryTimeout,
		DefaultMetricsFile: *defaultFileMetrics,
	}
	exporter, err := collector.NewExporter(logger, config)
	if err != nil {
		logger.Errorw("unable to connect to DB", err)
	}

	if *scrapeInterval != 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go exporter.RunScheduledScrapes(ctx, *scrapeInterval)
	}

	prometheus.MustRegister(exporter)
	prometheus.MustRegister(collectors.NewBuildInfoCollector())

	logger.Infow("Starting oracledb_exporter", "version", version.Info())
	logger.Infow("Build context", "build", version.BuildContext())
	logger.Infow("Collect from: ", "metricPath", *metricPath)

	opts := promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}
	http.Handle(*metricPath, promhttp.HandlerFor(prometheus.DefaultGatherer, opts))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><head><title>Oracle DB Exporter " + Version + "</title></head><body><h1>Oracle DB Exporter " + Version + "</h1><p><a href='" + *metricPath + "'>Metrics</a></p></body></html>"))
	})

	server := &http.Server{}
	httpLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := web.ListenAndServe(server, toolkitFlags, httpLogger); err != nil {
		logger.Errorw("msg", "Listening error", "reason", err)
		os.Exit(1)
	}
}

// getEnv returns the value of an environment variable, or returns the provided fallback value
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
