storage "raft" {
  path = "/openbao/file"
  node_id = "admin-01"
}

listener "tcp" {
  address = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_cert_file = "/openbao/tls/tls.crt"
  tls_key_file = "/openbao/tls/tls.key"
  telemetry {
    unauthenticated_metrics_access = true
  }
}

telemetry {
  prometheus_retention_time = "24h"
  disable_hostname = true
}

api_addr = "https://openbao:8200"
cluster_addr = "https://openbao:8201"
disable_mlock = false
ui = true
