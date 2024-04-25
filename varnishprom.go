// What would a line look like
// prom:deliver:host:backend:status:hit

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	log "log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metric struct {
	Description string                `json:"description"`
	Type        string                `json:"flag"`
	Format      string                `json:"format"`
	Value       uint64                `json:"value"`
	LastUpdated uint64                `json:"-"`
	Name        string                `json:"-"`
	LabelNames  []string              `json:"-"`
	LabelValues []string              `json:"-"`
	Source      string                `json:"-"`
	Gauge       prometheus.GaugeVec   `json:"-"`
	Counter     prometheus.CounterVec `json:"-"`
}

type VarnishStats74 struct {
	Version   int               `json:"version"`
	Timestamp string            `json:"timestamp"`
	Metrics   map[string]Metric `json:"counters"`
}

type GaugeOverView struct {
	Name       string
	Gauge      prometheus.GaugeVec
	Labels     []string
	Source     string
	LastUpdate uint64
}

type CounterOverView struct {
	Name       string
	Counter    prometheus.CounterVec
	Labels     []string
	Source     string
	LastUpdate uint64
}

type VarnishStats60 struct {
	//	Timestamp string `json:"-"`
	Metrics map[string]Metric
}

type VarnishStats interface {
	GetMetrics() map[string]Metric
}

type PromCounter struct {
	counterVec *prometheus.CounterVec
	LastUpdate uint64
}

func (v VarnishStats60) GetMetrics() map[string]Metric {
	return v.Metrics
}

func (v VarnishStats74) GetMetrics() map[string]Metric {
	return v.Metrics
}

var (
	dynamicGauges             = make(map[string]*prometheus.GaugeVec)
	dynamicGaugesMetricsMutex = &sync.Mutex{}
	dynamicCounters           = make(map[string]*PromCounter)
	dynamicCountsMetricsMutex = &sync.Mutex{}
	CounterOverViewMutex      = &sync.Mutex{}
	gaugeOverView             = make(map[string]*Metric)
	//	gaugeOverView   = make(map[string]int)
	activeVcl      = "boot"
	parsedVcl      = "boot"
	varnishVersion = "varnish-6.0.12"
	commitHash     = ""
	version        = "dev"     // goreleaser will fill this in
	commit         = "none"    // goreleaser will fill this in
	date           = "unknown" // goreleaser will fill this in
	tickerCount    = 0
)

// Create or get a reference to a existing gauge

func getGauge(key string, desc string, labelNames []string) *prometheus.GaugeVec {
	dynamicGaugesMetricsMutex.Lock()
	defer dynamicGaugesMetricsMutex.Unlock()

	// If the gauge already exists, return it
	if gauge, ok := dynamicGauges[key]; ok {
		return gauge
	}
	// Otherwise, create a new gauge
	gauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: key,
			Help: desc,
		},
		labelNames,
	)

	// Register the new gauge
	prometheus.MustRegister(gauge)
	// Store the new gauge in the map
	dynamicGauges[key] = gauge

	return gauge
}

func getCounter(key string, desc string, labelNames []string) *PromCounter {
	dynamicCountsMetricsMutex.Lock()
	defer dynamicCountsMetricsMutex.Unlock()

	// If the counter already exists, return it
	if counter, ok := dynamicCounters[key]; ok {
		return counter
	}

	// Otherwise, create a new counter
	var counter = new(PromCounter)
	counter.counterVec = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: key,
			Help: desc,
		},
		labelNames,
	)

	// Register the new counter
	prometheus.MustRegister(counter.counterVec)

	// Store the new counter in the map
	dynamicCounters[key] = counter

	return counter
}

/*
	 func setGauge(gauge *prometheus.GaugeVec, value uint64, labelNames []string) {
		dynamicGaugesMetricsMutex.Lock()
		defer dynamicGaugesMetricsMutex.Unlock()
		gauge.WithLabelValues(labelNames...).Set(float64(value))

}
*/
func setGauge(metric Metric) {
	gauge := getGauge(metric.Name, metric.Description, metric.LabelNames)
	gauge.WithLabelValues(metric.LabelValues...).Set(float64(metric.Value))
	metric.LastUpdated = uint64(tickerCount)
	metric.Gauge = *gauge
	identifier := metric.Name + strings.Join(metric.LabelValues, "")
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	sanitized := reg.ReplaceAllString(identifier, "")
	gaugeOverView[sanitized] = &metric
}

func main() {
	fqdn, _ := os.Hostname()
	shortName := strings.Split(fqdn, ".")[0]
	var listen = flag.String("i", "127.0.0.1:7083", "Listen interface for metrics endpoint")
	var path = flag.String("p", "/metrics", "Path for metrics endpoint")
	var logKey = flag.String("k", "prom", "logkey to look for promethus metrics")
	var logEnabled = flag.Bool("l", false, "Start varnishlog parser")
	var statEnabled = flag.Bool("s", false, "Start varnshstats parser")
	//	var varnishPath = flag.String("P", fqdn, "Path to varnish data")
	var adminHost = flag.String("T", "", "Varnish admin interface")
	var gitCheck = flag.String("g", "", "Check git commit hash of given directory")
	var secretsFile = flag.String("S", "/etc/varnish/secretsfile", "Varnish admin secret file")
	var versionFlag = flag.Bool("v", false, "Print version and exit")
	var hostname = flag.String("h", shortName, "Hostname to use in metrics, defaults to hostname -S")
	var collapse = flag.String("c", "^kozebamze$", "Regexp aganst director to collapse backend")
	var logLevel = flag.String("V", "info", "Loglevel for varnishprom (debug,info,warn,error)")
	flag.Parse()

	switch *logLevel {
	case "error":
		log.SetLogLoggerLevel(log.LevelError)
	case "warn":
		log.SetLogLoggerLevel(log.LevelWarn)
	case "debug":
		log.SetLogLoggerLevel(log.LevelDebug)
	}
	log.Debug("We are debugging")

	if *versionFlag {
		fmt.Printf("varnishprom version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	if *logEnabled {
		log.Info("Starting varnishlog parser", "logkey", *logKey)
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
				// Check if the line contains 'prom='
				keyIndex := strings.Index(line, *logKey+"=")
				if keyIndex != -1 {
					extracted := line[keyIndex+len(*logKey)+1:]

					// Split the extracted string into the counter name and labels
					parts := strings.SplitN(extracted, " ", 2)
					if len(parts) < 1 {
						// If there are not enough parts, skip this line
						continue
					}

					counterName := "varnishlog_" + strings.TrimSpace(parts[0])
					labels := strings.TrimSpace(parts[1])
					desc := "Varnishlog Counter"
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
						if labelName == "desc" {
							desc = labelValue
						}
						// Add the label name and value to the slices
						labelNames = append(labelNames, labelName)
						labelValues = append(labelValues, labelValue)
					}
					labelValues = append(labelValues, *hostname)
					labelNames = append(labelNames, "host")
					// Get the counter for this counter name
					counter := getCounter(counterName, desc, labelNames)

					// Increment the counter with the label values
					counter.counterVec.WithLabelValues(labelValues...).Inc()
					log.Debug("varnishlog", "id", counterName)
				}
			}
			log.Error("Lost connection to varnishlog")
		}()

		varnishlog.Start()
	}
	if *statEnabled {
		log.Info("Starting varnishstat parser")
		ticker := time.NewTicker(10 * time.Second)

		defer ticker.Stop()
		defer log.Info("Program is exiting")

		// Create a mutex
		var mutex sync.Mutex

		go func() {
			for range ticker.C {
				log.Debug("New varnishlog Tick")
				// Try to lock the mutex
				if !mutex.TryLock() {
					// If the mutex is already locked, skip this tick
					log.Warn("Mutex is locked, skipping tick, we might have a problem")
					continue
				}

				tickerCount++
				// Run the varnishadm command
				var varnishadm *exec.Cmd

				if *adminHost != "" {
					varnishadm = exec.Command("varnishadm", "-T", *adminHost, "-S", *secretsFile, "banner")
				} else {
					varnishadm = exec.Command("varnishadm", "banner")
				}
				varnishadmOutput, err := varnishadm.Output()

				if err != nil {
					log.Warn("Error running varnishadm", "err", err)
					log.Warn(fmt.Sprintf("varnishadm -T %s -S %s banner", *adminHost, *secretsFile))
					mutex.Unlock()
					continue
				}

				lines := strings.Split(string(varnishadmOutput), "\n")

				for _, line := range lines {
					// Check if the line starts with "active"
					if strings.HasPrefix(line, "varnish") {
						// Split the line by spaces and fetch the 4th column
						columns := strings.Fields(line)
						varnishVersion = columns[0]
					}
				}

				log.Debug("varnish", "version", varnishVersion)

				// Get the active VCL

				if *adminHost != "" {
					varnishadm = exec.Command("varnishadm", "-T", *adminHost, "-S", *secretsFile, "vcl.list")
				} else {
					varnishadm = exec.Command("varnishadm", "vcl.list")
				}
				varnishadmOutput, err = varnishadm.Output()

				if err != nil {
					log.Warn("Error running varnishadm: ", err)
					log.Warn(fmt.Sprintf("varnishadm -T %s -S %s vcl.list ", *adminHost, *secretsFile))
					break
				}

				// Split the output by lines
				lines = strings.Split(string(varnishadmOutput), "\n")

				// Iterate over the lines
				for _, line := range lines {
					// Check if the line starts with "active"
					if strings.HasPrefix(line, "active") {
						// Split the line by spaces and fetch the 4th column
						columns := strings.Fields(line)
						if len(columns) >= 5 {
							parsedVcl = columns[4]
							break
						} else if len(columns) >= 4 {
							// Varnish Enterprise
							parsedVcl = columns[3]
							break
						}

					}
				}
				log.Debug("VCL decifered", "parsedVcl", parsedVcl, "activeVcl", activeVcl)
				if parsedVcl != activeVcl {
					log.Info(fmt.Sprintf("Active VCL changed from %s to %s", activeVcl, parsedVcl))
					activeVcl = parsedVcl
				}

				// Get Commit hash if needed
				if *gitCheck != "" {
					// og -n 1 --pretty=format:"%H"
					gitCmd := exec.Command("git", "-C", *gitCheck, "log", "-n", "1", "--pretty=format:%H")
					gitCmdOutput, err := gitCmd.Output()
					if err != nil {
						log.Warn("Error running git: ", "error", err)
						break
					}
					commitHash = string(gitCmdOutput)
					setGauge(
						Metric{
							Name: "varnishstat_version", Description: "Version Varnish running",
							LabelNames:  []string{"version", "githash", "activevcl", "varnishprom", "host"},
							LabelValues: []string{varnishVersion, commitHash, activeVcl, version, *hostname},
						},
					)
				} else {
					log.Debug("We do not have a githash")
					setGauge(
						Metric{
							Name: "varnishstat_version", Description: "Version Varnish running",
							LabelNames:  []string{"version", "activevcl", "varnishprom", "host"},
							LabelValues: []string{varnishVersion, activeVcl, version, *hostname},
						},
					)
				}

				varnishstat := exec.Command("varnishstat", "-1", "-j")
				// Get a pipe connected to the command's standard output.
				varnishstatOutput, err := varnishstat.StdoutPipe()
				if err != nil {
					log.Warn("Failed varnishstat:", "error", err)
					break
				}
				if err := varnishstat.Start(); err != nil {
					log.Warn("Failed starting varnishstat:", "error", err)
					break
				}

				var stats VarnishStats
				if strings.Contains(varnishVersion, "6.0") {
					// We need to remove the dreaded timestamp line from varnishstat
					var filteredOutput bytes.Buffer
					scanner := bufio.NewScanner(varnishstatOutput)
					for scanner.Scan() {
						line := scanner.Text()
						if !strings.Contains(line, "timestamp") {
							filteredOutput.WriteString(line)
							filteredOutput.WriteString("\n")
						}
					}
					decoder := json.NewDecoder(bufio.NewReader(&filteredOutput))
					var stats6 VarnishStats60
					err = decoder.Decode(&stats6.Metrics)
					stats = stats6
				} else {
					var stats7 VarnishStats74
					decoder := json.NewDecoder(varnishstatOutput)
					err = decoder.Decode(&stats7)
					stats = stats7
				}
				if err != nil {
					log.Warn("Can't decode json from varnishstat", "error", err)
					return
				}
				// VBE.boot.goto.00000928.(52.2.2.2).(http://foobar.s3-website.eu-central-1.amazonaws.com:80).(ttl:10.000000).happy
				// VBE.boot.vglive_web_01.happy
				// VBE.boot.udo.vg_foobar_udo.(sa4:10.2.3.4:3005).happy
				gotoRe := regexp.MustCompile(`^.*\.goto\..*?\(([\d\.]+).*?\(([^\)]+).*\)\.(\w+)`)
				udoRe := regexp.MustCompile(`^.*\.udo\.(.*?)\.\(sa[46]:(\d+\.\d+\.\d+\.\d+:\d+)\)\.(\w+)`)
				backendRe := regexp.MustCompile(`^\w+\.\w+\.(\w+)\.(\w+)`)
				directorRe := regexp.MustCompile(`[-_\d]+$`)

				collapse := regexp.MustCompile(*collapse)

				//				bulkRe := regexp.MustCompile(`^(.*?)\s+(\d+)[\d\.\s]+(.*)`)
				metrics := stats.GetMetrics()
				for key, metric := range metrics {
					if metric.Type == "c" {
						if metric.Value == 0 && !strings.HasSuffix(key, ".req") {
							// skip enpty counters except req
							continue
						}
					}
					if strings.HasPrefix(key, "VBE."+activeVcl) {
						// We are in backend land
						var backend, director, counter, backendtype string

						if strings.HasPrefix(key, "VBE."+activeVcl+".udo") {
							backendtype = "udo"
							matched := udoRe.FindStringSubmatch(key)
							backend = matched[2]
							director = matched[1]
							counter = matched[3]
						} else if strings.HasPrefix(key, "VBE."+activeVcl+".goto") {
							backendtype = "goto"
							matched := gotoRe.FindStringSubmatch(key)
							backend = matched[1]
							director = matched[2]
							counter = matched[3]
						} else {
							backendtype = "single"
							matched := backendRe.FindStringSubmatch(key)
							backend = matched[1]
							director = backend
							suffix := directorRe.FindString(director)
							if suffix != "" {
								director = strings.TrimSuffix(director, suffix)
								backendtype = "simple"
							}
							counter = matched[2]
						}
						if collapse.MatchString(director) {
							backend = "<collapsed>"
						}
						if metric.Type == "c" {
							// Concatenate the failscenarios
							if strings.HasPrefix(counter, "fail_") {
								failtype := strings.TrimPrefix(counter, "fail_")
								metric.Name = "varnishstat_backend_fail"
								metric.LabelNames = []string{"backend", "director", "fail", "host", "type"}
								metric.LabelValues = []string{backend, director, failtype, *hostname, backendtype}
								setGauge(metric)
							} else {
								metric.Name = "varnishstat_backend_" + counter
								metric.LabelNames = []string{"backend", "director", "host", "type"}
								metric.LabelValues = []string{backend, director, *hostname, backendtype}
								setGauge(metric)
							}
						} else if metric.Type == "g" {
							metric.Name = "varnishstat_backend_" + counter
							metric.LabelNames = []string{"backend", "director", "host", "type"}
							metric.LabelValues = []string{backend, director, *hostname, backendtype}
							setGauge(metric)
						}
					} else if strings.HasPrefix(key, "VBE.") {
						// Not the current VCL. Skip these.
					} else {
						metric.Name = "varnishstat_" + strings.ReplaceAll(key, ".", "_")
						if metric.Type == "g" {
							metric.LabelNames = []string{"host"}
							metric.LabelValues = []string{*hostname}
							setGauge(metric)
						} else if metric.Type == "c" {
							metric.LabelNames = []string{"host"}
							metric.LabelValues = []string{*hostname}
							setGauge(metric)
						} else {
							log.Debug("Unknown metric type", "metrictype", metric.Type)
						}
					}
					// Add more conditions as needed.
				}
				log.Debug("Iterating over old Metrics")
				for metricname, metric := range gaugeOverView {
					if int(metric.LastUpdated) < tickerCount {
						metric.Gauge.DeleteLabelValues(metric.LabelValues...)
						// Delete the map object
						delete(gaugeOverView, metricname)
						log.Debug("Deleting old metrics ", "Metric", metricname)

					}

				}
				if err := varnishstat.Wait(); err != nil {
					log.Warn("Error waiting for varnishstat", "error", err)
				}
				mutex.Unlock()
			}
		}()
	}

	if *statEnabled || *logEnabled {
		// Set up Prometheus metrics endpoint
		log.Info("Starting Prometheus metrics endpoint on " + *listen + *path)
		http.Handle(*path, promhttp.Handler())
		err := http.ListenAndServe(*listen, nil)
		if err != nil {
			log.Error("Failed to start server:", "error", err)
		}
	} else {
		log.Error("Not starting log or statsparser. Enable -l (log) -s (stats) or both on the commandline")
		os.Exit(1)
	}
}
