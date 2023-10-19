# kubernetes-storage-metrics
Get prometheus metrics for PODs in kubernetes cluster

Source: https://github.com/jmcgrath207/k8s-ephemeral-storage-metrics

### Metriky

Name | Commment | Type
---|---|---
ephemeral_storage_pod_available | Dostupna kapacita ephemeral storage v PODu | gauge
ephemeral_storage_pod_capacity | Celkova kapacita ephemeral storage v PODu | gauge
ephemeral_storage_pod_usage | Obsazena kapacita ephemeral storage v PODu | gauge
volume_storage_pod_available | Dostupna kapacita referencovane storage pomoci PersistentVolumeClaim | gauge
volume_storage_pod_capacity | Celkova kapacita referencovane storage pomoci PersistentVolumeClaim | gauge
volume_storage_pod_usage | Dostupna kapacita referencovane storage pomoci PersistentVolumeClaim | gauge

#### Soucast ephemeral storage jsou data
- 

### Development
#### Spusteni v debug modu na lokalnim prostredi
```bash
LOG_LEVEL=debug IN_CLUSTER=false CURRENT_NODE_NAME=<name_k8s_node> go run main.go
```


#### Spusteni binarniho souboru na lokalni stanici s pristupem ke Kubernetes clusteru
Musime mit validne nakonfigurovany config pro kubernetes *soubor ~/.kube/config*
```bash
IN_CLUSTER=false CURRENT_NODE_NAME=<name_k8s_node> kubernetes-storage-metrics-<GOOS>-<GOARCH>
```

### Poznamky
- Scraper musi bezet jako DaemonSet na kazdem nodu