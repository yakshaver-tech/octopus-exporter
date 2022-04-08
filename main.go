package main

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"strings"
	"time"
  "fmt"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr             string
	apiKey           string
	mpan             string
	mprn             string
	electricityMeter string
	gasMeter         string
	debug            bool
)

type myCollector struct {
	metric *prometheus.Desc
}

func (c *myCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.metric
}

func (c *myCollector) Collect(ch chan<- prometheus.Metric) {
	if mpan != "" {
		timestamp, consumption, interval := getConsumption("electricity", mpan, electricityMeter)
		if !timestamp.IsZero() {
			log.Debug().Msgf("Electricity %f", consumption)

			s := prometheus.NewMetricWithTimestamp(
				timestamp,
				prometheus.MustNewConstMetric(
					c.metric,
					prometheus.GaugeValue,
					consumption,
					"electricity",
					mpan,
					electricityMeter,
					fmt.Sprint(interval),
				),
			)

			ch <- s
		}
	}
	if mprn != "" {
		timestamp, consumption, interval := getConsumption("gas", mprn, gasMeter)
		if !timestamp.IsZero() {
			log.Debug().Msgf("Gas %f", consumption)

			s := prometheus.NewMetricWithTimestamp(
				timestamp,
				prometheus.MustNewConstMetric(
					c.metric,
					prometheus.GaugeValue,
					consumption,
					"gas",
					mprn,
					gasMeter,
					fmt.Sprint(interval),
				),
			)

			ch <- s
		}
	}
}

func main() {
	flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
	flag.String("api-key", "", "The API key.")
	flag.String("mpan", "", "The MPAN.")
	flag.String("electricity-meter", "", "The electricity meter serial number.")
	flag.String("mprn", "", "The MPRN.")
	flag.String("gas-meter", "", "The gas meter serial number.")
	flag.Int("scrape-period", 60, "Time period between scrapes.")
	flag.Bool("debug", false, "Sets log level to debug.")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetEnvPrefix("OCTOPUS")
	viper.BindEnv("API-KEY")
	viper.BindEnv("MPAN")
	viper.BindEnv("ELECTRICITY-METER")
	viper.BindEnv("MPRN")
	viper.BindEnv("GAS-METER")
	viper.BindEnv("SCRAPE_PERIOD")
	viper.BindEnv("DEBUG")
	viper.BindPFlags(pflag.CommandLine)

	addr = viper.GetString("listen-address")
	apiKey = viper.GetString("api-key")
	mpan = viper.GetString("mpan")
	electricityMeter = viper.GetString("electricity-meter")
	mprn = viper.GetString("mprn")
	gasMeter = viper.GetString("gas-meter")
	debug = viper.GetBool("debug")

	if apiKey == "" {
		log.Fatal().Msg("api-key (OCTOPUS_API_KEY) must be set")
	}
	if mpan == "" && mprn == "" {
		log.Fatal().Msg("mpan or mprn (OCTOPUS_MPAN or OCTOPUS_MPRN) must be set")
	}
	if mpan != "" && electricityMeter == "" {
		log.Fatal().Msg("electricity-meter (OCTOPUS_ELECTRICITY_METER) must be set if mpan is set")
	}
	if mprn != "" && gasMeter == "" {
		log.Fatal().Msg("gas-meter (OCTOPUS_GAS_METER) must be set if mprn is set")
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().Msg("octopus-exporter starting")
	defer log.Info().Msg("octopus-exporter stopping")
	log.Info().Interface("settings", viper.AllSettings()).Send()

	collector := &myCollector{
		metric: prometheus.NewDesc(
			"octopus_consumption_kwh",
			"Energy consumption in kWh",
			[]string{"type", "point", "meter", "interval"},
			nil,
		),
	}
	prometheus.MustRegister(collector)

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal().Err(http.ListenAndServe(addr, nil))
}

func getConsumption(point string, number string, meter string) (time.Time, float64, float64) {
	url := "https://api.octopus.energy/v1/" + point + "-meter-points/" + number + "/meters/" + meter + "/consumption/"

	client := http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		log.Fatal().Err(err)
	}

	req.SetBasicAuth(apiKey, "")

	q := req.URL.Query()
	q.Add("page_size", "1")
	req.URL.RawQuery = q.Encode()

	res, err := client.Do(req)
	if err != nil {
		log.Fatal().Err(err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatal().Err(err)
	}
	// fmt.Printf("Status: %d\n", res.StatusCode)

	var bodyJson map[string]interface{}
	json.Unmarshal(body, &bodyJson)
	results := bodyJson["results"].([]interface{})
	if len(results) == 0 {
		return time.Time{}, 0.0, 0.0
	}
	result := results[0].(map[string]interface{})

	interval_start := result["interval_start"].(string)
	start, err := time.Parse(time.RFC3339, interval_start)
	if err != nil {
		log.Fatal().Err(err)
	}

	interval_end := result["interval_end"].(string)
	end, err := time.Parse(time.RFC3339, interval_end)
	if err != nil {
		log.Fatal().Err(err)
	}

	interval := end.Sub(start).Seconds()
	mid := start.Add(time.Duration(interval / 2) * time.Second)
	return mid, result["consumption"].(float64), float64(interval)
}
