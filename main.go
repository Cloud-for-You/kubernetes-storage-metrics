package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	inCluster      string
	clientset      *kubernetes.Clientset
	currentNode    string
	sampleInterval int64
)

type LineInfoHook struct{}

type pvcRef struct {
	Name           string `json:"name"`
}

type volumeStats struct {
	Name           string `json:"name"`	
	AvailableBytes float64 `json:"availableBytes"`
	CapacityBytes  float64 `json:"capacityBytes"`
	UsedBytes      float64 `json:"usedBytes"`
	PvcRef         pvcRef `json:"pvcRef"`
}

type ephemeralStorageMetrics struct {
	Node struct {
		NodeName string `json:"nodeName"`
	}
	Pods []struct {
		PodRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		}
		EphemeralStorage struct {
			AvailableBytes float64 `json:"availableBytes"`
			CapacityBytes  float64 `json:"capacityBytes"`
			UsedBytes      float64 `json:"usedBytes"`
		} `json:"ephemeral-storage"`
	}
}

type volumeStorageMetrics struct {
	Node struct {
		NodeName string `json:"nodeName"`
	}
	Pods []struct {
		PodRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		}
		Volumes			[]volumeStats `json:"volume"`         
	}
}

// Run implements zerolog.Hook.
func (LineInfoHook) Run(e *zerolog.Event, level zerolog.Level, message string) {
	panic("unimplemented")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func setLogger() {
	logLevel := getEnv("LOG_LEVEL", "info")
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		panic(err.Error())
	}
	zerolog.SetGlobalLevel(level)
	log.Hook(LineInfoHook{})
}

func getK8sClient() {
	inCluster = getEnv("IN_CLUSTER", "true")

	if inCluster == "true" {

		config, err := rest.InClusterConfig()
		if err != nil {
			log.Error().Msg("Failed to get rest config for in cluster client")
			panic(err.Error())
		}
		// creates the clientset
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			log.Error().Msg("Failed to get client set for in cluster client")
			panic(err.Error())
		}
		log.Debug().Msg("Successful got the in cluster client")

	} else {

		var kubeconfig *string
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		} else {
			kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
		}
		flag.Parse()

		// use the current context in kubeconfig
		config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			panic(err.Error())
		}

		// create the clientset
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

	}
}

func getEphemeralMetrics() {
	log.Debug().Msg("Starting Ephemeral Storage metrics collection")
	usedQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ephemeral_storage_pod_usage",
		Help: "Used to expose Ephemeral Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// Name of POD for Ephemeral Storage
			"pod",
			// Namespace of POD for Ephemeral Storage
			"namespace",
			// Name of Node where pod is placed
			"node",
		},
	)
	prometheus.MustRegister(usedQueued)

	capacityQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ephemeral_storage_pod_capacity",
		Help: "Capacity to expose Ephemeral Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// Name of POD for Ephemeral Storage
			"pod",
			// Namespace of POD for Ephemeral Storage
			"namespace",
			// Name of Node where pod is placed
			"node",
		},
	)
	prometheus.MustRegister(capacityQueued)

	availableQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ephemeral_storage_pod_available",
		Help: "Available to expose Ephemeral Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// Name of POD for Ephemeral Storage
			"pod",
			// Namespace of POD for Ephemeral Storage
			"namespace",
			// Name of Node where pod is placed
			"node",
		},
	)
	prometheus.MustRegister(availableQueued)

	currentNode = getEnv("CURRENT_NODE_NAME", "")
	intervalStr := getEnv("SCRAPE_DURATION", "15s")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Warn().Msgf("Invalid SCRAPE_DURATION '%s', using default 15s", intervalStr)
		interval = 15 * time.Second
	}
	sampleInterval := interval.Seconds()

	for {
		start := time.Now()
		// Get data
		content, err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/api/v1/nodes/%s/proxy/stats/summary", currentNode)).DoRaw(context.Background())
		if err != nil {
			log.Error().Msg(fmt.Sprintf("ErrorBadRequest : %s\n", err.Error()))
			os.Exit(1)
		}
		log.Debug().Msg(fmt.Sprintf("Fetched proxy stats from node : %s", currentNode))
		var data ephemeralStorageMetrics
		_ = json.Unmarshal(content, &data)
		usedQueued.Reset() // reset this metrics in the Exporter
		capacityQueued.Reset()
		availableQueued.Reset()
		nodeName := data.Node.NodeName

		for _, pod := range data.Pods {
			podName := pod.PodRef.Name
			podNamespace := pod.PodRef.Namespace
			usedBytes := pod.EphemeralStorage.UsedBytes
			capacityBytes := pod.EphemeralStorage.CapacityBytes
			availableBytes := pod.EphemeralStorage.AvailableBytes

			if podNamespace == "" || (usedBytes == 0 && availableBytes == 0 && capacityBytes == 0) {
				log.Warn().Msg(fmt.Sprintf("pod %s/%s on %s has no metrics on its ephemeral storage usage", podName, podNamespace, nodeName))
			}
			
			usedQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName}).Set(usedBytes)
			capacityQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName}).Set(capacityBytes)
			availableQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName}).Set(availableBytes)

			log.Debug().Msg(fmt.Sprintf("pod %s/%s on %s with usedBytes: %f", podNamespace, podName, nodeName, usedBytes))
		}

		// Use sleep
		elapsedTime := float64(time.Since(start).Milliseconds()) / 1000
		adjustTime := sampleInterval - elapsedTime
		log.Debug().Msgf("Adjusted Poll time: %f seconds", adjustTime)
		log.Debug().Msgf("Time Now: %f mil", elapsedTime)
		if adjustTime > 0 {
			time.Sleep(time.Duration(adjustTime * float64(time.Second)))
		}
	}
}

func getVolumeMetrics() {
	log.Debug().Msg("Starting Volume Storage metrics collection")
	usedQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "volume_storage_pod_usage",
		Help: "Used to expose Volume Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// Name of POD for Ephemeral Storage
			"pod",
			// Namespace of POD for Ephemeral Storage
			"namespace",
			// Name of Node where pod is placed
			"node",
			// Name of volume mount
			"volume_name",
			// Name of PVC
			"pvc_name",
		},
	)
	prometheus.MustRegister(usedQueued)

	capacityQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "volume_storage_pod_capacity",
		Help: "Capacity to expose Volume Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// name of pod for Volume Storage
			"pod",
			// namespace of pod for Volume Storage
			"namespace",
			// Name of Node where pod is placed.
			"node",
			// Name of volume mount
			"volume_name",
			// Name of PVC
			"pvc_name",
		},
	)
	prometheus.MustRegister(capacityQueued)

	availableQueued := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "volume_storage_pod_available",
		Help: "Available to expose Volume Storage metrics for pod ",
	},
		[]string{
			// Name of exporter
			"job",
			// name of pod for Volume Storage
			"pod",
			// namespace of pod for Volume Storage
			"namespace",
			// Name of Node where pod is placed.
			"node",
			// Name of volume mount
			"volume_name",
			// Name of PVC
			"pvc_name",
		},
	)
	prometheus.MustRegister(availableQueued)

	currentNode = getEnv("CURRENT_NODE_NAME", "")
	intervalStr := getEnv("SCRAPE_DURATION", "15s")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Warn().Msgf("Invalid SCRAPE_DURATION '%s', using default 15s", intervalStr)
		interval = 15 * time.Second
	}
	sampleInterval := interval.Seconds()

	for {
		start := time.Now()
		// Get data
		content, err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/api/v1/nodes/%s/proxy/stats/summary", currentNode)).DoRaw(context.Background())
		if err != nil {
			log.Error().Msg(fmt.Sprintf("ErrorBadRequst : %s\n", err.Error()))
			os.Exit(1)
		}
		log.Debug().Msg(fmt.Sprintf("Fetched proxy stats from node : %s", currentNode))
		var data volumeStorageMetrics
		_ = json.Unmarshal(content, &data)
		usedQueued.Reset() // reset this metrics in the Exporter
		capacityQueued.Reset()
		availableQueued.Reset()
		nodeName := data.Node.NodeName

		for _, pod := range data.Pods {
			podName := pod.PodRef.Name
			podNamespace := pod.PodRef.Namespace

			for _, volume := range pod.Volumes {
				if volume.PvcRef.Name != "" {
					volumeName := volume.Name 
					pvcName := volume.PvcRef.Name
					usedBytes := volume.UsedBytes
					capacityBytes := volume.CapacityBytes
					availableBytes := volume.AvailableBytes

					if podNamespace == "" || (usedBytes == 0 && availableBytes == 0 && capacityBytes == 0) {
						log.Warn().Msg(fmt.Sprintf("pod %s/%s on %s has no metrics on its pvcRef storage usage", podName, podNamespace, nodeName))
					}

					usedQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName, "volume_name": volumeName, "pvc_name": pvcName}).Set(usedBytes)
					capacityQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName, "volume_name": volumeName, "pvc_name": pvcName}).Set(capacityBytes)
					availableQueued.With(prometheus.Labels{"job": "kubernetes-storage-metrics", "namespace": podNamespace, "pod": podName, "node": nodeName, "volume_name": volumeName, "pvc_name": pvcName}).Set(availableBytes)

					log.Debug().Msg(fmt.Sprintf("pod %s/%s on %s with usedBytes: %f", podNamespace, podName, nodeName, usedBytes))
				}
			}
			
		}

		// Use sleep
		elapsedTime := float64(time.Since(start).Milliseconds()) / 1000
		adjustTime := sampleInterval - elapsedTime
		log.Debug().Msgf("Adjusted Poll time: %f seconds", adjustTime)
		log.Debug().Msgf("Time Now: %f mil", elapsedTime)
		if adjustTime > 0 {
			time.Sleep(time.Duration(adjustTime * float64(time.Second)))
		}
	}
}

func main() {
	flag.Parse()
	setLogger()

	// configure k8s client
	getK8sClient()

	// run parallel process for get metrics from system
	go getEphemeralMetrics()
	go getVolumeMetrics()

	// setup HTTP server
	port := getEnv("METRICS_PORT", "9100")
	http.Handle("/metrics", promhttp.Handler())
	log.Info().Msg(fmt.Sprintf("Starting server listening on :%s", port))
	err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	if err != nil {
		log.Error().Msg(fmt.Sprintf("Listener Failed : %s\n", err.Error()))
		panic(err.Error())
	}
}
