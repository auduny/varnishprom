// What would a line look like
// prom:deliver:host:backend:status:hit

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	dynamicGauges             = make(map[string]*prometheus.GaugeVec)
	dymamicGaugesMetricsMutex = &sync.Mutex{}
	dynamicCounters           = make(map[string]*prometheus.CounterVec)
	dynamicCountsMetricsMutex = &sync.Mutex{}
	hostname                  string
)

// Define a struct to hold the data.
type VarnishStatCounter struct {
	Description string `json:"description"`
	Flag        string `json:"flag"`
	Format      string `json:"format"`
	Value       int    `json:"value"`
}

type VarnishStatJson struct {
	Version   int                           `json:"version"`
	Timestamp string                        `json:"timestamp"`
	Counters  map[string]VarnishStatCounter `json:"counters"`
}

type VarnishPlusStatJson struct {
	Version   int                           `json:"version"`
	Timestamp string                        `json:"timestamp"`
	Counters  map[string]VarnishStatCounter `json:"counters"`
}

func getGauge(key string, labelNames []string) *prometheus.GaugeVec {
	dymamicGaugesMetricsMutex.Lock()
	defer dymamicGaugesMetricsMutex.Unlock()

	// If the gauge already exists, return it
	if gauge, ok := dynamicGauges[key]; ok {
		return gauge
	}
	labelNames = append(labelNames, "host")
	// Otherwise, create a new gauge
	gauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("varnish%s", key),
			Help: fmt.Sprintf("Gauge of key %s in varnishstat output", key),
		},
		labelNames,
	)

	// Register the new gauge
	prometheus.MustRegister(gauge)

	// Store the new gauge in the map
	dynamicGauges[key] = gauge

	return gauge
}

func setGaugeValue(key string, value float64) {
	gauge, err := dynamicGauges[key].GetMetricWithLabelValues(key)
	if err != nil {
		// handle error
		return
	}
	gauge.Set(value)
}

func getCounter(key string, labelNames []string) *prometheus.CounterVec {
	dynamicCountsMetricsMutex.Lock()
	defer dynamicCountsMetricsMutex.Unlock()

	// If the counter already exists, return it
	if counter, ok := dynamicCounters[key]; ok {
		return counter
	}
	labelNames = append(labelNames, "host")

	// Otherwise, create a new counter
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("varnish%s", key),
			Help: fmt.Sprintf("Counts of key %s in varnishlog output", key),
		},
		labelNames,
	)

	// Register the new counter
	prometheus.MustRegister(counter)

	// Store the new counter in the map
	dynamicCounters[key] = counter

	return counter
}

func main() {
	fqdn, _ := os.Hostname()
	hostname = strings.Split(fqdn, ".")[0]
	// Start varnishlog as a subprocess
	varnishlog := exec.Command("varnishlog", "-i", "VCL_Log")
	varnishlogOutput, err := varnishlog.StdoutPipe()
	if err != nil {
		panic(err)
	}

	scanner := bufio.NewScanner(varnishlogOutput)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			// Check if the line contains 'prom:'
			keyIndex := strings.Index(line, "prom=")
			if keyIndex != -1 {
				extracted := line[keyIndex+len("prom="):]

				// Split the extracted string into the counter name and labels
				parts := strings.SplitN(extracted, " ", 2)
				if len(parts) < 1 {
					// If there are not enough parts, skip this line
					continue
				}

				counterName := "log_" + strings.TrimSpace(parts[0])
				labels := strings.TrimSpace(parts[1])

				// Split the labels into pairs
				labelPairs := strings.Split(labels, ",")

				// Create slices to hold the label names and values
				labelNames := make([]string, 0, len(labelPairs))
				labelValues := make([]string, 0, len(labelPairs))
				for _, pair := range labelPairs {
					// Split the pair into the label name and value
					pairParts := strings.SplitN(pair, "=", 2)
					if len(pairParts) < 2 {
						// If there are not enough parts, skip this pair
						continue
					}

					labelName := pairParts[0]
					labelValue := pairParts[1]

					// Add the label name and value to the slices
					labelNames = append(labelNames, labelName)
					labelValues = append(labelValues, labelValue)
				}
				labelValues = append(labelValues, hostname)
				// Get the counter for this counter name
				counter := getCounter(counterName, labelNames)

				// Increment the counter with the label values
				counter.WithLabelValues(labelValues...).Inc()
			}
		}
	}()

	varnishlog.Start()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer fmt.Println("Program is exiting")

	// Create a mutex
	var mutex sync.Mutex

	go func() {
		for range ticker.C {
			// Try to lock the mutex
			if !mutex.TryLock() {
				// If the mutex is already locked, skip this tick
				fmt.Println("Mutex is locked, skipping tick, we might have a problem")
				continue
			}

			// Run the varnishadm command
			varnishadm := exec.Command("varnishadm", "vcl.list")
			varnishadmOutput, err := varnishadm.Output()
			if err != nil {
				panic(err)
			}

			// Split the output by lines
			lines := strings.Split(string(varnishadmOutput), "\n")
			activeVcl := "boot"
			// Iterate over the lines
			for _, line := range lines {
				// Check if the line starts with "active"
				if strings.HasPrefix(line, "active") {
					// Split the line by spaces and fetch the 4th column
					columns := strings.Fields(line)
					if len(columns) >= 5 {
						activeVcl = columns[4]
						break
					} else if len(columns) >= 4 {
						// Varnish Enterprise
						activeVcl = columns[3]
						break
					}

				}
			}

			fmt.Println("Active VCL is", activeVcl)

			varnishstat := exec.Command("varnishstat", "-1", "-j")
			// Get a pipe connected to the command's standard output.
			varnishstatoutput, err := varnishstat.StdoutPipe()
			if err != nil {
				log.Fatal(err)
			}

			// Start the command.
			if err := varnishstat.Start(); err != nil {
				log.Fatal(err)
			}

			// Create a JSON decoder for the output.
			decoder := json.NewDecoder(varnishstatoutput)
			var data VarnishPlusStatJson
			if err := decoder.Decode(&data); err != nil {
				log.Fatal(err)
			}

			for key, counter := range data.Counters {
				// Check the key and the flag of the counter.
				// Goto backend
				if strings.HasPrefix(key, "VBE."+activeVcl+".goto") && counter.Value > 0 {
					split := strings.Split(key, ".")
					backend := strings.TrimSuffix(strings.TrimPrefix(split[4], "("), ")")
					director := strings.TrimSuffix(strings.TrimPrefix(split[5], "("), ")")
					fmt.Print("backend: ", backend, " director: ", director, " value: ", counter.Value, "\n")
					metric := getGauge("stats_backend", []string{backend, director})
					metric.WithLabelValues(backend, director).Set(float64(counter.Value))
				} else if strings.HasPrefix(key, "VBE."+activeVcl) && counter.Value > 0 {
					split := strings.Split(key, ".")
					backend := split[2]
					director := strings.TrimSuffix(split[2], "1")
					stat := split[3]
					metric := getGauge("statsn_backend", []string{"backend", "director", "stat"})
					metric.WithLabelValues(backend, director, stat, hostname).Set(float64(counter.Value))
				} else {

				}
				// Add more conditions as needed.
			}

			if err := varnishstat.Wait(); err != nil {
				log.Fatal(err)
			}
			mutex.Unlock()
		}
	}()

	// Set up Prometheus metrics endpoint

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe("127.0.0.1:8083", nil)
}
