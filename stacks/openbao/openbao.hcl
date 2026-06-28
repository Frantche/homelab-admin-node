storage "raft" {
  path = "/openbao/file"
  node_id = "admin-01"
}

listener "tcp" {
  address = "0.0.0.0:8200"
  tls_disable = 1
  telemetry {
    unauthenticated_metrics_access = true
  }
}

telemetry {
  prometheus_retention_time = "24h"
  disable_hostname = true
}

api_addr = "http://openbao:8200"
cluster_addr = "http://openbao:8201"
ui = true
